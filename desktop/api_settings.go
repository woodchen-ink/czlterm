package main

import (
	"runtime"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/autostart"
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
	OS        string            `json:"os"`
	// AutoStartSupported 为 false 时界面不显示开机自启; AutoStart 读自系统登记, 不在 settings 里。
	AutoStartSupported bool `json:"autoStartSupported"`
	AutoStart          bool `json:"autoStart"`
}

// GetSettings 返回设置与可选的终端、编辑器。MCP 令牌不下发, 界面只显示配置片段。
func (a *App) GetSettings() (SettingsView, error) {
	if err := a.ready(); err != nil {
		return SettingsView{}, err
	}
	s := a.settings.Get()
	s.MCPToken = ""
	auto, err := autostart.Enabled()
	if err != nil {
		a.log.Warn("read autostart", "err", err)
	}
	return SettingsView{Settings: s, Terminals: launch.Terminals(), Editors: launch.Editors(), DataDir: a.dataDir, OS: runtime.GOOS,
		AutoStartSupported: autostart.Supported(), AutoStart: auto}, nil
}

// SetAutoStart 立即登记或取消登录自启, 自启后窗口隐藏在托盘。返回系统里的实际状态。
func (a *App) SetAutoStart(on bool) (bool, error) {
	if err := autostart.Set(on); err != nil {
		a.log.Warn("set autostart", "on", on, "err", err)
		return false, err
	}
	return autostart.Enabled()
}

// SaveSettings 保存设置并让改动立即生效。
func (a *App) SaveSettings(in settings.Settings) (settings.Settings, error) {
	if err := a.ready(); err != nil {
		return in, err
	}
	prev := a.settings.Get()
	next, err := a.settings.Update(func(s *settings.Settings) {
		token, space := s.MCPToken, s.Space
		*s = in
		// 令牌只由程序生成与轮换, 界面传回的是被清空的值; 当前空间由空间切换器单独维护。
		s.MCPToken, s.Space = token, space
	})
	if err != nil {
		return prev, err
	}
	a.vault.Configure(next.BWPath, time.Duration(next.VaultAutoLockMinutes)*time.Minute)
	// 「记住解锁」开关: 打开时若已解锁立即保存当前会话, 关闭时删除保存的会话。
	switch {
	case next.VaultRemember && !prev.VaultRemember:
		a.rememberVault()
	case !next.VaultRemember && prev.VaultRemember:
		_ = secret.Set(secret.VaultSessionKey, "")
	}
	// 关闭国家查询时清掉本机缓存的 IP 与国家。
	if !next.GeoLookup && prev.GeoLookup {
		a.geo.Clear()
	}
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
