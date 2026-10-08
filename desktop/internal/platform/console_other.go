//go:build !windows

package platform

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

func hideConsole(*exec.Cmd) {}

// ImportLoginPath 把登录 shell 的 PATH 并入当前进程。
//
// 从 Finder / Launchpad 启动的 .app 只拿到 launchd 的精简 PATH (/usr/bin:/bin:...),
// 找不到 Homebrew 或 npm 装的 bw、git, 而 bw 本身是 node 脚本, 还要能找到 node。
// 读不到时保持原样, 设置页里仍可手填 bw 路径。
func ImportLoginPath() {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -i 让 zsh 读 .zshrc (nvm、fnm 之类通常写在这里); 标记行用来从 rc 文件的杂散输出里取出 PATH。
	out, err := exec.CommandContext(ctx, shell, "-ilc", `printf '__CZLTERM_PATH__%s\n' "$PATH"`).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "__CZLTERM_PATH__"); ok && p != "" {
			merged := mergePath(p, os.Getenv("PATH"))
			os.Setenv("PATH", merged)
			return
		}
	}
}

// mergePath 以 login 为先合并两个 PATH 并去重。
func mergePath(login, current string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(strings.Split(login, ":"), strings.Split(current, ":")...) {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ":")
}
