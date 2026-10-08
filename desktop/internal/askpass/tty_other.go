//go:build !windows

package askpass

import "os"

// openTTY 打开控制终端。ssh 调 askpass 时子进程仍挂在同一个终端上。
func openTTY() (tty, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return tty{}, err
	}
	return tty{in: f, out: f, fd: int(f.Fd()), close: func() { f.Close() }}, nil
}
