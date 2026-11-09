//go:build !windows

package term

import (
	"syscall"
	"time"
)

// killTree 命令以独立会话启动 (go-pty 设置 Setsid), 进程组号即 pid:
// 先 SIGHUP 让它像关窗口一样收尾, 再 SIGKILL 整组。
func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	time.Sleep(200 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
