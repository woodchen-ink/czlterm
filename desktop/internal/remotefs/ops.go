package remotefs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

// ChmodScope 是修改权限的作用范围。
type ChmodScope string

const (
	ChmodSelf  ChmodScope = "self"  // 只改本项
	ChmodAll   ChmodScope = "all"   // 本项及所有子目录、子文件
	ChmodDirs  ChmodScope = "dirs"  // 本项及所有子目录
	ChmodFiles ChmodScope = "files" // 只改所有子文件, 本目录不动
)

// Chmod 修改权限。mode 为 POSIX 权限位 (含 setuid/setgid/sticky, 最大 07777)。
// 递归时不跟随符号链接: 对链接 chmod 会改到链接指向的文件, 可能落在目录树之外。
func Chmod(c *sftp.Client, p string, mode uint32, scope ChmodScope) error {
	if mode > 0o7777 {
		return errors.New("5125 invalid permission mode")
	}
	p = clean(p)
	m := os.FileMode(mode)
	switch scope {
	case ChmodSelf:
		return wrap("5121 change permissions", c.Chmod(p, m))
	case ChmodAll, ChmodDirs, ChmodFiles:
	default:
		return errors.New("5126 invalid permission scope")
	}
	fi, err := c.Stat(p)
	if err != nil {
		return wrap("5102 stat file", err)
	}
	if !fi.IsDir() {
		return wrap("5121 change permissions", c.Chmod(p, m))
	}
	w := c.Walk(p)
	for w.Step() {
		if err := w.Err(); err != nil {
			return wrap("5101 list directory", err)
		}
		st := w.Stat()
		if st.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		dir := st.IsDir()
		if (scope == ChmodDirs && !dir) || (scope == ChmodFiles && (dir || !st.Mode().IsRegular())) {
			continue
		}
		if err := c.Chmod(w.Path(), m); err != nil {
			return wrap("5121 change permissions", fmt.Errorf("%s: %w", w.Path(), err))
		}
	}
	return nil
}

// Copy 把 src (文件或目录, 递归) 复制到 dstDir 下, 返回新路径。目标重名时自动编号 ("a (1).txt"),
// 所以在同一目录复制粘贴即得到副本。SFTP 没有服务端复制, 数据会经本机中转。
// 中途失败时删除已复制的部分, 不留下半个目录。
func Copy(c *sftp.Client, src, dstDir string) (string, error) {
	src, dstDir = clean(src), clean(dstDir)
	if err := checkTarget(src, dstDir); err != nil {
		return "", err
	}
	dst, err := uniqueName(c, dstDir, path.Base(src))
	if err != nil {
		return "", err
	}
	if err := copyTree(c, src, dst); err != nil {
		_ = removeTree(c, dst)
		return "", err
	}
	return dst, nil
}

// Move 把 src 移到 dstDir 下, 保持原名, 返回新路径。目标已存在时报错, 不覆盖。
func Move(c *sftp.Client, src, dstDir string) (string, error) {
	src, dstDir = clean(src), clean(dstDir)
	if err := checkTarget(src, dstDir); err != nil {
		return "", err
	}
	dst := path.Join(dstDir, path.Base(src))
	if dst == src {
		return dst, nil
	}
	if _, err := c.Lstat(dst); err == nil {
		return "", fmt.Errorf("5122 target already exists: %s", dst)
	}
	return dst, wrap("5127 move", c.Rename(src, dst))
}

// checkTarget 拒绝操作根目录, 以及把目录复制或移动到它自己的子树里 (会无限递归)。
func checkTarget(src, dstDir string) error {
	if src == "/" || src == "." {
		return errors.New("5123 cannot copy or move this folder")
	}
	if dstDir == src || strings.HasPrefix(dstDir, src+"/") {
		return errors.New("5123 cannot copy or move a folder into itself")
	}
	return nil
}

// uniqueName 在 dir 下给 name 找一个不存在的路径: name, "stem (1).ext", "stem (2).ext" …
func uniqueName(c *sftp.Client, dir, name string) (string, error) {
	stem, ext := name, ""
	// 只把可见文件的末尾扩展名拆出来; ".bashrc" 整体当文件名。
	if e := path.Ext(name); e != "" && e != name {
		stem, ext = strings.TrimSuffix(name, e), e
	}
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		p := path.Join(dir, candidate)
		if _, err := c.Lstat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return p, nil
			}
			return "", wrap("5102 stat file", err)
		}
	}
	return "", fmt.Errorf("5122 target already exists: %s", path.Join(dir, name))
}

// copyTree 递归复制, 保留权限; 符号链接按链接本身复制, 不复制其指向的内容。
func copyTree(c *sftp.Client, src, dst string) error {
	fi, err := c.Lstat(src)
	if err != nil {
		return wrap("5102 stat file", err)
	}
	mode := fi.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		target, err := c.ReadLink(src)
		if err != nil {
			return wrap("5119 resolve symlink", err)
		}
		return wrap("5124 copy", c.Symlink(target, dst))
	case mode.IsDir():
		if err := c.Mkdir(dst); err != nil {
			return wrap("5114 create directory", err)
		}
		infos, err := c.ReadDir(src)
		if err != nil {
			return wrap("5101 list directory", err)
		}
		for _, child := range infos {
			if err := copyTree(c, path.Join(src, child.Name()), path.Join(dst, child.Name())); err != nil {
				return err
			}
		}
		// 子项写完再设权限, 只读目录也能复制。
		_ = c.Chmod(dst, mode)
		return nil
	case mode.IsRegular():
		return copyFile(c, src, dst, mode)
	default:
		return fmt.Errorf("5124 copy: unsupported file type: %s", src)
	}
}

func copyFile(c *sftp.Client, src, dst string, mode fs.FileMode) error {
	in, err := c.Open(src)
	if err != nil {
		return wrap("5105 open file", err)
	}
	defer in.Close()
	out, err := c.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return wrap("5106 create file", err)
	}
	if _, err := in.WriteTo(out); err != nil {
		out.Close()
		return wrap("5124 copy", err)
	}
	if err := out.Close(); err != nil {
		return wrap("5124 copy", err)
	}
	_ = c.Chmod(dst, mode)
	return nil
}

// removeTree 递归删除, 只用于清理本次新建的副本。用 Lstat 判断类型, 不进入符号链接指向的目录;
// sftp.Client.RemoveAll 用 Stat, 遇到指向目录的链接会删到链接外面去。
func removeTree(c *sftp.Client, p string) error {
	fi, err := c.Lstat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return c.Remove(p)
	}
	infos, err := c.ReadDir(p)
	if err != nil {
		return err
	}
	for _, child := range infos {
		if err := removeTree(c, path.Join(p, child.Name())); err != nil {
			return err
		}
	}
	return c.RemoveDirectory(p)
}

// permBits 把 Go 的 FileMode 还原成 POSIX 权限位, 供界面按八进制展示与编辑。
func permBits(m fs.FileMode) uint32 {
	bits := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits
}
