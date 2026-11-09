//go:build windows

package term

import (
	"os/exec"
	"strconv"
	"syscall"
)

// killTree 用 taskkill /T 连同子进程一起结束: 关闭 ConPTY 只会结束直接挂在上面的进程,
// ssh 拉起的 ssh-proxy helper 会留下。
func killTree(pid int) {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Run()
}
