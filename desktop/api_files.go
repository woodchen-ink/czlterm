package main

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/pkg/sftp"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/remotefs"
)

const fileOpTimeout = 2 * time.Minute

// sftpFor 取连接的 SFTP 客户端。只有 SSH 连接有文件管理。
func (a *App) sftpFor(ctx context.Context, id string) (*sftp.Client, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	c, err := a.conns.Get(id)
	if err != nil {
		return nil, err
	}
	if c.Protocol != conn.ProtoSSH {
		return nil, errors.New("5120 file manager is only available for ssh connections")
	}
	hops, err := a.resolveChain(c)
	if err != nil {
		return nil, err
	}
	chain, err := sshHops(hops)
	if err != nil {
		return nil, err
	}
	return a.pool.SFTP(ctx, c.ID, chain)
}

// withSFTP 执行一次文件操作; 失败时丢弃该连接, 下次重连 (多半是断线)。
func (a *App) withSFTP(id string, fn func(*sftp.Client) error) error {
	ctx, cancel := context.WithTimeout(a.ctx, fileOpTimeout)
	defer cancel()
	c, err := a.sftpFor(ctx, id)
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		var se *sftp.StatusError
		if !errors.As(err, &se) {
			a.pool.Drop(id)
		}
		return err
	}
	return nil
}

// FilesList 列出远程目录。dir 为空时列登录用户的起始目录。
func (a *App) FilesList(id, dir string) (remotefs.Listing, error) {
	var out remotefs.Listing
	err := a.withSFTP(id, func(c *sftp.Client) error {
		if dir == "" {
			dir, _ = remotefs.Home(c)
		}
		var err error
		out, err = remotefs.List(c, dir)
		return err
	})
	return out, err
}

// FilesMkdir 创建目录。
func (a *App) FilesMkdir(id, p string) error {
	return a.withSFTP(id, func(c *sftp.Client) error { return remotefs.Mkdir(c, p) })
}

// FilesRename 改名或移动。
func (a *App) FilesRename(id, from, to string) error {
	return a.withSFTP(id, func(c *sftp.Client) error { return remotefs.Rename(c, from, to) })
}

// FilesMove 把多个文件或目录移到 dir 下, 返回成功移动的数量; 遇到第一个失败即停止。
func (a *App) FilesMove(id string, srcs []string, dir string) (int, error) {
	return a.eachSFTP(id, srcs, func(c *sftp.Client, src string) error {
		_, err := remotefs.Move(c, src, dir)
		return err
	})
}

// FilesCopy 把多个文件或目录复制到 dir 下, 重名时自动编号。返回成功复制的数量。
func (a *App) FilesCopy(id string, srcs []string, dir string) (int, error) {
	return a.eachSFTP(id, srcs, func(c *sftp.Client, src string) error {
		_, err := remotefs.Copy(c, src, dir)
		return err
	})
}

// FilesChmod 修改权限。scope 见 remotefs.ChmodScope。
func (a *App) FilesChmod(id string, paths []string, mode uint32, scope string) (int, error) {
	return a.eachSFTP(id, paths, func(c *sftp.Client, p string) error {
		return remotefs.Chmod(c, p, mode, remotefs.ChmodScope(scope))
	})
}

// eachSFTP 对每个路径依次执行操作, 返回成功的数量。
func (a *App) eachSFTP(id string, paths []string, fn func(*sftp.Client, string) error) (int, error) {
	n := 0
	err := a.withSFTP(id, func(c *sftp.Client) error {
		for _, p := range paths {
			if err := fn(c, p); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// FilesRemove 删除文件或空目录。
func (a *App) FilesRemove(id, p string) error {
	return a.withSFTP(id, func(c *sftp.Client) error { return remotefs.Remove(c, p) })
}

// FilesDownload 弹出保存对话框并下载文件。用户取消时返回空串。
func (a *App) FilesDownload(id, p string) (string, error) {
	local, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{DefaultFilename: path.Base(p)})
	if err != nil || local == "" {
		return "", err
	}
	return local, a.withSFTP(id, func(c *sftp.Client) error { return remotefs.Download(c, p, local) })
}

// FilesUpload 弹出打开对话框, 把选中的文件上传到 dir。返回上传成功的文件数。
func (a *App) FilesUpload(id, dir string) (int, error) {
	files, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{})
	if err != nil || len(files) == 0 {
		return 0, err
	}
	n := 0
	err = a.withSFTP(id, func(c *sftp.Client) error {
		for _, f := range files {
			if _, err := remotefs.Upload(c, f, dir); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
