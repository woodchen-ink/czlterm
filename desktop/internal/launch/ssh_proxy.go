package launch

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/askpass"
)

// sshProxySpec 只保存配置路径及短期回调令牌, 不保存密码或私钥。
type sshProxySpec struct {
	Binary string   `json:"binary"`
	Args   []string `json:"args"`
	Env    []string `json:"env"`
}

// SSHPasswordEnv 返回应用原有的密码回调环境, 供系统认证跳恢复, 避免继承其他跳的令牌。
func SSHPasswordEnv() []string {
	var env []string
	for _, name := range sshPasswordEnvNames() {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// sshPasswordEnvNames 是必须逐跳重置的本地密码回调变量, 不是远程 SetEnv。
func sshPasswordEnvNames() []string {
	return []string{"SSH_AUTH_SOCK", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "DISPLAY", askpass.EnvURL, askpass.EnvToken}
}

// sshProxyEnvironment 清除父跳密码作用域, 再恢复该跳自己明确指定的环境。
func sshProxyEnvironment(inherited, scoped []string) []string {
	var env []string
	for _, entry := range inherited {
		name, _, _ := strings.Cut(entry, "=")
		reset := false
		for _, reserved := range sshPasswordEnvNames() {
			if strings.EqualFold(name, reserved) {
				reset = true
				break
			}
		}
		if !reset {
			env = append(env, entry)
		}
	}
	return append(env, scoped...)
}

// RunSSHProxy 以原始标准流转发二进制 SSH 隧道, 不通过 PowerShell 或 cmd 包装环境。
// args 是 manifest 路径、转发目标主机、转发端口; 此入口在 Wails 初始化前执行。
func RunSSHProxy(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "4043 invalid ssh proxy arguments")
		return 255
	}
	file, err := os.Open(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "4043 read ssh proxy config")
		return 255
	}
	var spec sshProxySpec
	err = json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&spec)
	_ = file.Close()
	if err != nil || spec.Binary == "" || len(spec.Args) == 0 {
		fmt.Fprintln(os.Stderr, "4043 invalid ssh proxy config")
		return 255
	}
	commandArgs := append([]string{"-W", net.JoinHostPort(args[1], args[2])}, spec.Args...)
	cmd := exec.Command(spec.Binary, commandArgs...)
	cmd.Env = sshProxyEnvironment(os.Environ(), spec.Env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() > 0 {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "4043 start ssh proxy")
		return 255
	}
	return 0
}
