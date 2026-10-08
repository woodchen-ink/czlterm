package platform

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// ImportLoginPath 在 Windows 上无事可做: 从开始菜单启动的程序本就继承用户 PATH。
func ImportLoginPath() {}
