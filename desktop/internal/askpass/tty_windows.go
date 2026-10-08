package askpass

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentProcess 是 AttachConsole 的 ATTACH_PARENT_PROCESS ((DWORD)-1)。
const attachParentProcess = ^uintptr(0)

// openTTY 附着到父进程 (ssh.exe) 的控制台。本程序是 GUI 子系统程序, 默认没有控制台。
func openTTY() (tty, error) {
	_, _, _ = procAttachConsole.Call(attachParentProcess)
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return tty{}, err
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return tty{}, err
	}
	return tty{in: in, out: out, fd: int(in.Fd()), close: func() { in.Close(); out.Close() }}, nil
}
