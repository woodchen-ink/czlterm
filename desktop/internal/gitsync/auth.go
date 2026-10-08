package gitsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// Auth 是同步用的可选凭据。两项都为空时完全沿用用户自己的 git 配置 (SSH 密钥、凭据助手)。
type Auth struct {
	// Username 用于 HTTPS 仓库; SSH 仓库的用户名写在地址里, 忽略此项。
	Username string
	// Secret 对 HTTPS 仓库是访问令牌或密码, 对 SSH 仓库是私钥 (OpenSSH / PEM 文本)。
	Secret string
	// Exe 是本程序路径, 作为 GIT_ASKPASS 回答 HTTPS 的用户名与密码提示。
	Exe string
}

// 给 GIT_ASKPASS 子进程的环境变量。
const (
	envGitAskpass = "CZLTERM_GIT_ASKPASS"
	envGitUser    = "CZLTERM_GIT_USER"
	envGitSecret  = "CZLTERM_GIT_SECRET"
)

// isSSHRemote 判断仓库地址是否走 SSH: ssh:// 或 scp 风格的 user@host:path。
func isSSHRemote(remote string) bool {
	if strings.HasPrefix(remote, "ssh://") {
		return true
	}
	if strings.Contains(remote, "://") {
		return false
	}
	at, colon := strings.Index(remote, "@"), strings.Index(remote, ":")
	return colon > 0 && (at < 0 || at < colon)
}

// prepare 返回本次同步要附加给 git 的配置参数与环境变量, 以及结束后的清理函数。
func (a Auth) prepare(remote string) (args, env []string, cleanup func(), err error) {
	cleanup = func() {}
	env = []string{"GIT_TERMINAL_PROMPT=0"}
	if a.Secret == "" && a.Username == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		return nil, env, cleanup, nil
	}
	if isSSHRemote(remote) {
		if a.Secret == "" {
			env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
			return nil, env, cleanup, nil
		}
		dir, err := paths.PrivateTemp()
		if err != nil {
			return nil, nil, cleanup, err
		}
		f, err := os.CreateTemp(dir, "git-key-*")
		if err != nil {
			return nil, nil, cleanup, fmt.Errorf("6010 write git key: %w", err)
		}
		key := strings.TrimSpace(strings.ReplaceAll(a.Secret, "\r\n", "\n")) + "\n"
		_, werr := f.WriteString(key)
		f.Close()
		cleanup = func() { os.Remove(f.Name()) }
		if werr != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("6010 write git key: %w", werr)
		}
		_ = os.Chmod(f.Name(), 0o600)
		keyPath := filepath.ToSlash(f.Name())
		env = append(env, `GIT_SSH_COMMAND=ssh -i "`+keyPath+`" -o IdentitiesOnly=yes -o BatchMode=yes`)
		return nil, env, cleanup, nil
	}
	// HTTPS: 清空用户已配置的凭据助手, 由本程序作为 GIT_ASKPASS 回答用户名与令牌。
	args = []string{"-c", "credential.helper="}
	env = append(env, "GIT_ASKPASS="+a.Exe, envGitAskpass+"=1", envGitUser+"="+a.Username, envGitSecret+"="+a.Secret)
	return args, env, cleanup, nil
}

// AskpassActive 报告当前进程是否作为 git 的 GIT_ASKPASS 被调起。
func AskpassActive() bool { return os.Getenv(envGitAskpass) == "1" }

// RunAskpass 回答 git 的提示: "Username for ..." 回用户名, 其余 (Password for ...) 回密钥。
func RunAskpass(args []string) int {
	prompt := strings.ToLower(strings.Join(args, " "))
	if strings.HasPrefix(prompt, "username") {
		fmt.Println(os.Getenv(envGitUser))
	} else {
		fmt.Println(os.Getenv(envGitSecret))
	}
	return 0
}
