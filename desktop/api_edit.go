package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
	"github.com/woodchen-ink/czlterm/desktop/internal/remotefs"
)

// 远程文件编辑: 下载到本地缓存, 用外部编辑器打开, 轮询本地文件的修改时间, 保存即传回。
//
// 不用文件系统通知: 不少编辑器保存时先写临时文件再改名, 原文件句柄上的通知会丢;
// 每秒比较一次修改时间与大小足够及时, 也不依赖平台差异。

const (
	editMaxBytes = 20 << 20
	editPoll     = time.Second
)

type editState struct {
	mu       sync.Mutex
	sessions map[string]*editSession
}

type editSession struct {
	ConnID string `json:"connId"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
	// LastSaved 为最近一次成功传回的时间 (RFC3339), 尚未传回过时为空。
	LastSaved string `json:"lastSaved"`
	// Error 为最近一次传回失败的原因, 成功后清空。
	Error string `json:"error"`

	cancel context.CancelFunc
}

// EditStatus 是推给前端的编辑状态。
type EditStatus struct {
	ConnID    string `json:"connId"`
	Remote    string `json:"remote"`
	LastSaved string `json:"lastSaved"`
	Error     string `json:"error"`
}

// FilesEdit 用设置里的编辑器打开远程文件, 保存后自动传回。同一文件重复打开时复用本地副本。
// 会话持续到用户在界面上结束编辑或程序退出; 不设超时, 免得编辑器还开着时本地副本被删。
func (a *App) FilesEdit(id, remote string) error {
	key := id + ":" + remote
	cfg := a.settings.Get()

	// 查重与占位在同一把锁内完成: 连点两次不会建出两个会话。
	a.edits.mu.Lock()
	if a.edits.sessions == nil {
		a.edits.sessions = map[string]*editSession{}
	}
	if existing := a.edits.sessions[key]; existing != nil {
		local := existing.Local
		a.edits.mu.Unlock()
		if local == "" {
			return nil // 第一次打开还在下载, 下载完会自己打开编辑器
		}
		return launch.OpenInEditor(cfg.Editor, cfg.EditorPath, local)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &editSession{ConnID: id, Remote: remote, cancel: cancel}
	a.edits.sessions[key] = s
	a.edits.mu.Unlock()

	local, mod, size, err := a.downloadForEdit(id, remote, key)
	if err != nil {
		a.edits.stopIf(key, s)
		return err
	}
	a.edits.mu.Lock()
	s.Local = local
	a.edits.mu.Unlock()
	go a.watchEdit(ctx, key, s, mod, size)

	return launch.OpenInEditor(cfg.Editor, cfg.EditorPath, local)
}

// downloadForEdit 把远程文件下载到 cache/edit/<hash>/ 下, 返回本地路径与写入后的修改时间、大小。
func (a *App) downloadForEdit(id, remote, key string) (string, time.Time, int64, error) {
	dir, err := paths.Cache("edit")
	if err != nil {
		return "", time.Time{}, 0, err
	}
	sum := sha256.Sum256([]byte(key))
	dir = filepath.Join(dir, hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", time.Time{}, 0, err
	}
	local := filepath.Join(dir, localName(path.Base(remote)))

	var data []byte
	if err := a.withSFTP(id, func(c *sftp.Client) error {
		var err error
		data, err = remotefs.ReadFile(c, remote, editMaxBytes)
		return err
	}); err != nil {
		return "", time.Time{}, 0, err
	}
	if err := os.WriteFile(local, data, 0o600); err != nil {
		return "", time.Time{}, 0, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	return local, fi.ModTime(), fi.Size(), nil
}

// FilesEditSessions 列出正在编辑的远程文件。
func (a *App) FilesEditSessions() []EditStatus {
	a.edits.mu.Lock()
	defer a.edits.mu.Unlock()
	out := []EditStatus{}
	for _, s := range a.edits.sessions {
		out = append(out, EditStatus{ConnID: s.ConnID, Remote: s.Remote, LastSaved: s.LastSaved, Error: s.Error})
	}
	return out
}

// FilesEditStop 结束对一个远程文件的编辑跟踪并删除本地副本。
func (a *App) FilesEditStop(id, remote string) {
	a.edits.stop(id + ":" + remote)
}

func (e *editState) stop(key string) {
	e.stopIf(key, nil)
}

// stopIf 结束 key 的会话; only 非空时仅当当前会话就是 only 才结束 (监视协程退出时, 同一文件可能已被重新打开)。
func (e *editState) stopIf(key string, only *editSession) {
	e.mu.Lock()
	s := e.sessions[key]
	if only != nil && s != only {
		e.mu.Unlock()
		return
	}
	delete(e.sessions, key)
	e.mu.Unlock()
	if s != nil {
		s.cancel()
		if s.Local != "" {
			_ = os.RemoveAll(filepath.Dir(s.Local))
		}
	}
}

func (e *editState) stopAll() {
	e.mu.Lock()
	keys := make([]string, 0, len(e.sessions))
	for k := range e.sessions {
		keys = append(keys, k)
	}
	e.mu.Unlock()
	for _, k := range keys {
		e.stop(k)
	}
}

// watchEdit 轮询本地副本, 变化且两次轮询间大小稳定后传回。
func (a *App) watchEdit(ctx context.Context, key string, s *editSession, mod time.Time, size int64) {
	defer a.edits.stopIf(key, s)
	t := time.NewTicker(editPoll)
	defer t.Stop()
	pendingSize := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		fi, err := os.Stat(s.Local)
		if err != nil {
			// 编辑器改名保存的瞬间文件可能不存在, 下一轮再看。
			continue
		}
		if fi.ModTime().Equal(mod) && fi.Size() == size {
			pendingSize = -1
			continue
		}
		// 等一轮确认写完, 避免把编辑器写了一半的内容传上去。
		if pendingSize != fi.Size() {
			pendingSize = fi.Size()
			continue
		}
		pendingSize = -1
		// 只有上传成功才记下新的修改时间: 失败 (断线、保险库已锁) 时下一轮会重试, 不让这次保存悄悄丢掉。
		if a.uploadEdit(s) {
			mod, size = fi.ModTime(), fi.Size()
		}
	}
}

// uploadEdit 把本地副本传回服务器, 返回是否成功。失败只在错误变化时通知界面, 避免每秒重试刷屏。
func (a *App) uploadEdit(s *editSession) bool {
	data, err := os.ReadFile(s.Local)
	if err == nil {
		err = a.withSFTP(s.ConnID, func(c *sftp.Client) error { return remotefs.WriteFile(c, s.Remote, data) })
	}
	a.edits.mu.Lock()
	prevErr := s.Error
	if err != nil {
		s.Error = err.Error()
		if errors.Is(err, context.Canceled) {
			s.Error = "upload cancelled"
		}
	} else {
		s.Error = ""
		s.LastSaved = time.Now().UTC().Format(time.RFC3339)
	}
	st := EditStatus{ConnID: s.ConnID, Remote: s.Remote, LastSaved: s.LastSaved, Error: s.Error}
	a.edits.mu.Unlock()
	if err != nil {
		if st.Error == prevErr {
			return false
		}
		a.log.Warn("upload edited file", "conn", s.ConnID, "err", err)
	}
	wruntime.EventsEmit(a.ctx, eventEditStatus, st)
	return err == nil
}

// localName 把远程文件名转成本地可用的文件名: Windows 不允许 <>:"/\|?* 与控制字符。
// 保留扩展名, 编辑器靠它识别语法。
func localName(name string) string {
	out := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}
