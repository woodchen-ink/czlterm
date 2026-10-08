package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/gitsync"
	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
	"github.com/woodchen-ink/czlterm/desktop/internal/settings"
)

// SettingsView 是设置页的数据: 设置本身加上可选项。
type SettingsView struct {
	Settings  settings.Settings `json:"settings"`
	Terminals []launch.Terminal `json:"terminals"`
	Editors   []launch.Editor   `json:"editors"`
	DataDir   string            `json:"dataDir"`
	// HasGitSecret 表示系统密钥库里存有同步密钥。密钥本身不下发。
	HasGitSecret bool   `json:"hasGitSecret"`
	OS           string `json:"os"`
}

// GetSettings 返回设置与可选的终端、编辑器。MCP 令牌不下发, 界面只显示配置片段。
func (a *App) GetSettings() (SettingsView, error) {
	if err := a.ready(); err != nil {
		return SettingsView{}, err
	}
	s := a.settings.Get()
	s.MCPToken = ""
	gs, _ := secret.Get(secret.GitKey)
	return SettingsView{Settings: s, Terminals: launch.Terminals(), Editors: launch.Editors(), DataDir: a.dataDir, OS: runtime.GOOS,
		HasGitSecret: gs != ""}, nil
}

// SetGitSecret 保存同步密钥 (HTTPS 访问令牌或 SSH 私钥) 到系统密钥库; 传空串则删除, 恢复使用 git 默认认证。
func (a *App) SetGitSecret(value string) error {
	return secret.Set(secret.GitKey, strings.TrimSpace(value))
}

// SaveSettings 保存设置并让改动立即生效。
func (a *App) SaveSettings(in settings.Settings) (settings.Settings, error) {
	if err := a.ready(); err != nil {
		return in, err
	}
	prev := a.settings.Get()
	next, err := a.settings.Update(func(s *settings.Settings) {
		token := s.MCPToken
		*s = in
		// 令牌只由程序生成与轮换, 界面传回的是被清空的值。
		s.MCPToken = token
	})
	if err != nil {
		return prev, err
	}
	a.vault.Configure(next.BWPath, time.Duration(next.VaultAutoLockMinutes)*time.Minute)
	if next.MCPEnabled && !prev.MCPEnabled {
		if err := a.startMCP(); err != nil {
			return next, err
		}
	} else if !next.MCPEnabled && prev.MCPEnabled {
		a.stopMCP()
	}
	next.MCPToken = ""
	return next, nil
}

// RevealDataDir 在文件管理器中打开数据目录。
func (a *App) RevealDataDir() error {
	return launch.Reveal(filepath.Join(a.dataDir, "connections"))
}

// SyncStatus 是同步状态。
type SyncStatus struct {
	Running  bool   `json:"running"`
	LastSync string `json:"lastSync"`
	Error    string `json:"error"`
}

type syncState struct {
	mu      sync.Mutex
	status  SyncStatus
	timer   *time.Timer
	running sync.Mutex
}

// GetSyncStatus 返回最近一次同步的结果。
func (a *App) GetSyncStatus() SyncStatus {
	a.sync.mu.Lock()
	defer a.sync.mu.Unlock()
	return a.sync.status
}

// SyncNow 立即同步。同一时间只跑一次, 后到的请求等前一次结束后再跑。
func (a *App) SyncNow() (SyncStatus, error) {
	if err := a.ready(); err != nil {
		return SyncStatus{}, err
	}
	a.sync.running.Lock()
	defer a.sync.running.Unlock()

	a.setSync(func(s *SyncStatus) { s.Running = true })
	cfg := a.settings.Get()
	auth := gitsync.Auth{Username: cfg.GitUsername}
	var err error
	if auth.Secret, err = secret.Get(secret.GitKey); err == nil {
		auth.Exe, err = os.Executable()
	}
	if err == nil {
		err = a.conns.Exclusive(func() error {
			return gitsync.Sync(context.Background(), a.dataDir, cfg.GitRemote, auth)
		})
	}
	a.setSync(func(s *SyncStatus) {
		s.Running = false
		if err != nil {
			s.Error = err.Error()
			return
		}
		s.Error = ""
		s.LastSync = time.Now().UTC().Format(time.RFC3339)
	})
	if err != nil {
		a.log.Warn("sync", "err", err)
	}
	return a.GetSyncStatus(), err
}

func (a *App) setSync(fn func(*SyncStatus)) {
	a.sync.mu.Lock()
	fn(&a.sync.status)
	st := a.sync.status
	a.sync.mu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, eventSyncStatus, st)
	}
}

// scheduleAutoSync 在开启自动同步且配置了远端时, 于最后一次修改 3 秒后同步: 连续编辑只同步一次。
func (a *App) scheduleAutoSync() {
	cfg := a.settings.Get()
	if !cfg.GitAutoSync || cfg.GitRemote == "" {
		return
	}
	a.sync.mu.Lock()
	defer a.sync.mu.Unlock()
	if a.sync.timer != nil {
		a.sync.timer.Stop()
	}
	a.sync.timer = time.AfterFunc(3*time.Second, func() { _, _ = a.SyncNow() })
}
