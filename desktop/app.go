package main

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/agent"
	"github.com/woodchen-ink/czlterm/desktop/internal/askpass"
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/facts"
	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
	"github.com/woodchen-ink/czlterm/desktop/internal/settings"
	"github.com/woodchen-ink/czlterm/desktop/internal/sshx"
	"github.com/woodchen-ink/czlterm/desktop/internal/vault"
)

// 推给前端的事件名。
const (
	eventVaultLocked   = "vault:locked"
	eventVaultUnlocked = "vault:unlocked"
	eventSyncStatus    = "sync:status"
	eventEditStatus    = "edit:status"
	eventFacts         = "facts:updated"
)

// App 承载应用生命周期, 方法绑定给前端调用。
//
// 绑定方法一律返回 (数据, error): Wails 把 error 转成前端 Promise 的 reject,
// 错误串以四位数字码开头, 前端据此展示带码的提示。
type App struct {
	ctx context.Context
	log *slog.Logger

	conns    *conn.Store
	scripts  *script.Store
	facts    *facts.Store
	settings *settings.Store
	vault    *vault.Vault
	agents   *agent.Server
	askpass  *askpass.Broker
	pool     *sshx.Pool
	dataDir  string

	mcp     mcpState
	edits   editState
	sync    syncState
	updates updateState

	// initErr 是启动阶段的致命错误 (数据目录不可用), 界面据此显示错误页而不是空列表。
	initErr error

	// quitting 在退出流程中置位: 退出时也会锁定保险库, 但不该删掉「记住解锁」保存的会话。
	quitting atomic.Bool
}

// NewApp 创建 App。重活放在 startup, 以便窗口先出来。
func NewApp() *App {
	return &App{log: newLogger()}
}

// startup 由 Wails 在窗口创建后调用。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.agents = agent.New()
	a.pool = sshx.NewPool()
	a.vault = vault.New(a.onVaultLocked)

	if err := a.init(); err != nil {
		a.initErr = err
		a.log.Error("startup", "err", err)
		return
	}
	cfg := a.settings.Get()
	a.vault.Configure(cfg.BWPath, time.Duration(cfg.VaultAutoLockMinutes)*time.Minute)
	if cfg.MCPEnabled {
		if err := a.startMCP(); err != nil {
			a.log.Error("start mcp", "err", err)
		}
	}
	if cfg.VaultRemember {
		go a.restoreVault()
	}
	go a.runUpdateChecks(ctx)
	a.startTray()
	a.log.Info("startup done", "version", version, "os", runtime.GOOS)
}

func (a *App) init() error {
	cfgDir, err := paths.Config()
	if err != nil {
		return err
	}
	a.settings, err = settings.Open(cfgDir, settings.Settings{
		Terminal:             launch.DefaultTerminal(),
		Editor:               "vscode",
		VaultAutoLockMinutes: 60,
	})
	if err != nil {
		// 设置文件损坏时用默认值继续, 不让整个程序起不来。
		a.log.Warn("load settings", "err", err)
	}
	if a.dataDir, err = paths.Data(); err != nil {
		return err
	}
	if a.conns, err = conn.Open(a.dataDir); err != nil {
		return err
	}
	if a.scripts, err = script.Open(a.dataDir); err != nil {
		return err
	}
	cacheDir, err := paths.Sub("cache")
	if err != nil {
		return err
	}
	if a.facts, err = facts.Open(a.dataDir, cacheDir); err != nil {
		return err
	}
	if a.askpass, err = askpass.Start(); err != nil {
		return err
	}
	return nil
}

// shutdown 清理内存里的凭据与本地端点。
func (a *App) shutdown(context.Context) {
	a.quitting.Store(true)
	a.stopTray()
	a.stopMCP()
	a.edits.stopAll()
	a.pool.CloseAll()
	a.agents.CloseAll()
	a.vault.Lock()
	if a.askpass != nil {
		a.askpass.Close()
	}
}

// onVaultLocked 在保险库锁定 (手动或自动) 后丢弃所有用保险库凭据建立的东西。
func (a *App) onVaultLocked() {
	// 手动锁定与空闲超时都意味着「现在不想保持解锁」, 保存的会话一并作废; 退出程序不算。
	if !a.quitting.Load() {
		_ = secret.Set(secret.VaultSessionKey, "")
	}
	if a.askpass != nil {
		a.askpass.RevokeAll()
	}
	a.agents.CloseAll()
	a.pool.CloseAll()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, eventVaultLocked)
	}
}

// ready 在启动失败时返回统一错误, 绑定方法开头调用。
func (a *App) ready() error {
	if a.initErr != nil {
		return a.initErr
	}
	if a.conns == nil {
		return errors.New("1010 app is still starting")
	}
	return nil
}

// clipboard 用 Wails runtime 实现 launch.Clipboard。
type clipboard struct{ ctx context.Context }

func (c clipboard) SetText(s string) error { return wruntime.ClipboardSetText(c.ctx, s) }
func (c clipboard) Text() (string, error)  { return wruntime.ClipboardGetText(c.ctx) }
