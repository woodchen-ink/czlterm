// Package platform 收口与操作系统相关的小工具: 子进程不弹控制台、继承登录 shell 的 PATH。
package platform

import (
	"context"
	"os/exec"
)

// Command 创建子进程, Windows 下不弹出控制台窗口。用于 bw / git / cmdkey 这类后台调用。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideConsole(cmd)
	return cmd
}
