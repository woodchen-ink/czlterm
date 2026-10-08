package launch

import (
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// newConsole 让 PowerShell 开在自己的新控制台窗口里。
func newConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
