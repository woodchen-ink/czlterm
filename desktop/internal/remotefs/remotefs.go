// Package remotefs 封装文件管理用到的 SFTP 操作。
package remotefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// Entry 是目录中的一项。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	IsLink  bool   `json:"isLink"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
}

// Listing 是一次列目录的结果。
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// Home 返回登录用户的起始目录。
func Home(c *sftp.Client) (string, error) {
	wd, err := c.Getwd()
	if err != nil {
		return "/", nil
	}
	return wd, nil
}

// List 列目录, 目录在前、按名称排序。符号链接会解析一次以判断是否指向目录。
func List(c *sftp.Client, dir string) (Listing, error) {
	dir = clean(dir)
	infos, err := c.ReadDir(dir)
	if err != nil {
		return Listing{}, wrap("5101 list directory", err)
	}
	out := Listing{Path: dir, Entries: make([]Entry, 0, len(infos))}
	for _, fi := range infos {
		e := Entry{
			Name:    fi.Name(),
			Path:    path.Join(dir, fi.Name()),
			IsDir:   fi.IsDir(),
			IsLink:  fi.Mode()&fs.ModeSymlink != 0,
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime().UTC().Format(time.RFC3339),
		}
		if e.IsLink {
			if target, err := c.Stat(e.Path); err == nil {
				e.IsDir = target.IsDir()
			}
		}
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// ReadFile 读取不超过 max 字节的文件; 超过时返回错误而不是截断, 避免 AI 拿着半个文件改写后整体覆盖回去。
func ReadFile(c *sftp.Client, p string, max int64) ([]byte, error) {
	p = clean(p)
	fi, err := c.Stat(p)
	if err != nil {
		return nil, wrap("5102 stat file", err)
	}
	if fi.IsDir() {
		return nil, errors.New("5103 path is a directory")
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("5104 file is larger than %d bytes", max)
	}
	f, err := c.Open(p)
	if err != nil {
		return nil, wrap("5105 open file", err)
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max+1))
}

// WriteFile 先写同目录临时文件再改名覆盖, 中途断线不会留下写了一半的目标文件。
//
//   - 目标是符号链接时写到链接指向的真实文件 (如 nginx sites-enabled), 不把链接替换成普通文件
//   - 临时文件名随机且以 O_EXCL 创建: 以 root 写全局可写目录时, 别人预先放好的同名链接不会被跟随截断
//   - 已存在的文件保留原权限
//   - 服务器不支持原子覆盖时, 先把原文件改名为备份再换上新文件, 任何一步失败都把原文件挪回去
func WriteFile(c *sftp.Client, p string, data []byte) error {
	p = clean(p)
	p, err := resolveLinks(c, p)
	if err != nil {
		return err
	}

	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".czlterm-"+randSuffix())
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return wrap("5106 create file", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		c.Remove(tmp)
		return wrap("5107 write file", err)
	}

	if err := f.Close(); err != nil {
		c.Remove(tmp)
		return wrap("5107 write file", err)
	}
	if fi, err := c.Stat(p); err == nil {
		_ = c.Chmod(tmp, fi.Mode().Perm())
	}
	if err := c.PosixRename(tmp, p); err == nil {
		return nil
	}

	// 无原子覆盖: 原文件 → 备份, 临时文件 → 原名, 删备份。
	backup := tmp + ".bak"
	hadOriginal := c.Rename(p, backup) == nil
	if err := c.Rename(tmp, p); err != nil {
		if hadOriginal {
			_ = c.Rename(backup, p)
		}
		c.Remove(tmp)
		return wrap("5108 replace file", err)
	}
	if hadOriginal {
		_ = c.Remove(backup)
	}
	return nil
}

// resolveLinks 沿符号链接找到真实文件, 最多跟 16 层。不用 RealPath: 部分服务器的 realpath 只规范化路径、不解析链接。
func resolveLinks(c *sftp.Client, p string) (string, error) {
	for range 16 {
		fi, err := c.Lstat(p)
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			return p, nil // 不存在 (新建文件) 或已是普通文件
		}
		target, err := c.ReadLink(p)
		if err != nil {
			return "", wrap("5119 resolve symlink", err)
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		p = path.Clean(target)
	}
	return "", errors.New("5119 resolve symlink: too many levels of symbolic links")
}

func randSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Download 把远程文件保存到本地路径。
func Download(c *sftp.Client, remote, local string) error {
	src, err := c.Open(clean(remote))
	if err != nil {
		return wrap("5105 open file", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("5109 create local file: %w", err)
	}
	if _, err := src.WriteTo(dst); err != nil {
		dst.Close()
		return wrap("5110 download", err)
	}
	return dst.Close()
}

// Upload 把本地文件上传到远程目录, 返回远程路径。
func Upload(c *sftp.Client, local, remoteDir string) (string, error) {
	src, err := os.Open(local)
	if err != nil {
		return "", fmt.Errorf("5111 open local file: %w", err)
	}
	defer src.Close()
	if fi, err := src.Stat(); err == nil && fi.IsDir() {
		return "", errors.New("5112 uploading folders is not supported")
	}
	remote := path.Join(clean(remoteDir), baseName(local))
	dst, err := c.OpenFile(remote, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return "", wrap("5106 create file", err)
	}
	if _, err := dst.ReadFrom(src); err != nil {
		dst.Close()
		return "", wrap("5113 upload", err)
	}
	return remote, dst.Close()
}

// Mkdir 创建目录。
func Mkdir(c *sftp.Client, p string) error {
	return wrap("5114 create directory", c.Mkdir(clean(p)))
}

// Rename 改名或移动。
func Rename(c *sftp.Client, from, to string) error {
	return wrap("5115 rename", c.Rename(clean(from), clean(to)))
}

// Remove 删除文件或空目录。不做递归删除: 误点一次就清空整个目录的代价太大。
func Remove(c *sftp.Client, p string) error {
	p = clean(p)
	if p == "/" {
		return errors.New("5116 refusing to remove /")
	}
	fi, err := c.Lstat(p)
	if err != nil {
		return wrap("5102 stat file", err)
	}
	if fi.IsDir() {
		return wrap("5117 remove directory (must be empty)", c.RemoveDirectory(p))
	}
	return wrap("5118 remove file", c.Remove(p))
}

// Stat 返回单个路径的信息。
func Stat(c *sftp.Client, p string) (Entry, error) {
	p = clean(p)
	fi, err := c.Stat(p)
	if err != nil {
		return Entry{}, wrap("5102 stat file", err)
	}
	return Entry{Name: fi.Name(), Path: p, IsDir: fi.IsDir(), Size: fi.Size(), Mode: fi.Mode().String(),
		ModTime: fi.ModTime().UTC().Format(time.RFC3339)}, nil
}

// clean 规范化远程路径。远程总是 POSIX 风格, 不能用 filepath。
func clean(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return "."
	}
	return path.Clean(p)
}

// baseName 取本地路径的文件名, 兼容 Windows 分隔符。
func baseName(local string) string {
	local = strings.ReplaceAll(local, "\\", "/")
	return path.Base(local)
}

// wrap 给错误加码; err 为 nil 时返回 nil。权限与不存在两类错误单独标出, 界面据此给出可读提示。
func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: not found: %w", what, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: permission denied: %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}
