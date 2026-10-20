package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// SSHHop 描述一跳的认证边界; Agent 只能是该跳保险库私钥的专用端点。
type SSHHop struct {
	Host     string
	Port     int
	User     string
	Vault    bool
	Password bool
	Agent    string
	// Env 是该跳自己的密码回调环境, 不继承其他跳的凭据作用域。
	Env []string
}

// SSHCommand 为每一跳准备独立的认证和代理, 返回目标 SSH 参数及需要清理的文件。
// 文件不含密码或私钥; proxy manifest 中的短期回调令牌仍按敏感文件保管。
func SSHCommand(binary, helper string, hops []SSHHop) (args, files []string, err error) {
	if len(hops) == 0 {
		return nil, nil, fmt.Errorf("5002 empty connection chain")
	}
	dir, err := paths.PrivateTemp()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			for _, path := range files {
				_ = os.Remove(path)
			}
		}
	}()
	for i, h := range hops {
		var proxy string
		if i > 0 {
			manifest := filepath.Join(dir, "ssh-proxy-"+randHex()+".json")
			body, marshalErr := json.Marshal(sshProxySpec{Binary: binary, Args: args, Env: hops[i-1].Env})
			if marshalErr != nil {
				return nil, files, fmt.Errorf("4042 encode ssh proxy config: %w", marshalErr)
			}
			files = append(files, manifest)
			if err = writeSSHPrivateFile(manifest, body); err != nil {
				return nil, files, fmt.Errorf("4042 write ssh proxy config: %w", err)
			}
			proxy = sshProxyCommand(helper, manifest, runtime.GOOS)
		}
		if h.Vault {
			body, configErr := sshHopConfig(h, proxy)
			if configErr != nil {
				return nil, files, configErr
			}
			path := filepath.Join(dir, "ssh-"+randHex()+".config")
			files = append(files, path)
			if err = writeSSHPrivateFile(path, []byte(body)); err != nil {
				return nil, files, fmt.Errorf("4042 write ssh config: %w", err)
			}
			args = []string{"-F", path}
		} else {
			// 不用 -F 模拟 Include, 保留 OpenSSH 的用户/系统配置解析上下文。
			args = systemSSHArgs(h, proxy)
		}
		args = append(args, "--", h.Host)
	}
	return args, files, nil
}

// systemSSHArgs 只覆盖连接管理器明确指定的目标与跳板, 认证完全交给 OpenSSH。
func systemSSHArgs(h SSHHop, proxy string) []string {
	args := []string{"-p", strconv.Itoa(h.Port), "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3"}
	if h.User != "" {
		args = append(args, "-l", h.User)
	}
	if proxy != "" {
		args = append(args, "-o", "ProxyCommand="+proxy)
	}
	return args
}

// sshHopConfig 仅用于保险库认证, 不 Include 用户或系统 SSH 配置。
func sshHopConfig(h SSHHop, proxy string) (string, error) {
	if !h.Password && h.Agent == "" {
		return "", fmt.Errorf("4108 vault credentials are required")
	}
	if strings.Contains(h.Agent, "${") || strings.ContainsAny(h.Agent, "\r\n") {
		return "", fmt.Errorf("4042 unsupported ssh agent path")
	}
	var b strings.Builder
	b.WriteString("Host *\n")
	fmt.Fprintf(&b, "  Port %d\n", h.Port)
	if h.User != "" {
		fmt.Fprintf(&b, "  User %s\n", sshConfigQuote(h.User))
	}
	b.WriteString("  ServerAliveInterval 30\n  ServerAliveCountMax 3\n")
	if proxy != "" {
		fmt.Fprintf(&b, "  ProxyCommand %s\n", proxy)
	}
	// -F 隔离 ~/.ssh/config; IdentityFile none 禁止默认磁盘私钥。
	b.WriteString("  IdentityFile none\n  CertificateFile none\n  GSSAPIAuthentication no\n  HostbasedAuthentication no\n")
	var methods []string
	if h.Agent != "" {
		fmt.Fprintf(&b, "  IdentityAgent %s\n", sshConfigQuote(strings.ReplaceAll(h.Agent, "%", "%%")))
		b.WriteString("  IdentitiesOnly no\n  PubkeyAuthentication yes\n")
		methods = append(methods, "publickey")
	} else {
		b.WriteString("  IdentityAgent none\n  PubkeyAuthentication no\n")
	}
	if h.Password {
		b.WriteString("  PasswordAuthentication yes\n  KbdInteractiveAuthentication yes\n  NumberOfPasswordPrompts 1\n")
		methods = append(methods, "keyboard-interactive", "password")
	} else {
		b.WriteString("  PasswordAuthentication no\n  KbdInteractiveAuthentication no\n")
	}
	fmt.Fprintf(&b, "  PreferredAuthentications %s\n", strings.Join(methods, ","))
	return b.String(), nil
}

// sshConfigQuote 保护配置值中的空格、引号和反斜杠; 不接受跨行内容。
func sshConfigQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(value) + `"`
}

// sshProxyCommand 经本程序的无 GUI helper 启动上一跳, 保留目标地址的 OpenSSH 占位符。
func sshProxyCommand(helper, manifest, goos string) string {
	args := []string{helper, "ssh-proxy", filepath.ToSlash(manifest), "%h", "%p"}
	for i, arg := range args {
		if i < 3 {
			arg = strings.ReplaceAll(arg, "%", "%%")
		}
		if goos == "windows" {
			// Windows OpenSSH 直接创建进程, 不经 cmd 或 PowerShell。
			args[i] = `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
		} else {
			args[i] = shQuote(arg)
		}
	}
	return strings.Join(args, " ")
}
