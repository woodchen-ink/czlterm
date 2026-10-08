//go:build !windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// PrivateTemp 返回 $TMPDIR/czlterm-<uid>, 仅当前用户可访问。
//
// 放 ssh-agent socket 与终端启动脚本: 安装目录路径含空格 ("Application Support"),
// 既可能超过 Unix socket 104 字节的路径上限, 也容易在拼给终端的命令里被拆开。
func PrivateTemp() (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("czlterm-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("1006 create private temp dir: %w", err)
	}
	// 目录可能是别人预先建好的: 确认属主与权限, 否则 socket 与脚本可被他人读写。
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("1006 create private temp dir: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return "", fmt.Errorf("1007 unsafe private temp dir %s", dir)
	}
	return dir, nil
}
