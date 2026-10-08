package main

import (
	"context"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
	"github.com/woodchen-ink/czlterm/desktop/internal/vault"
)

// VaultStatus 返回保险库状态 (是否装了 bw、是否登录、是否已解锁)。
func (a *App) VaultStatus() vault.Status {
	return a.vault.Status(a.ctx)
}

// VaultUnlock 用主密码解锁。主密码只经环境变量交给 bw, 不落盘、不记日志。
func (a *App) VaultUnlock(password string) error {
	if err := a.vault.Unlock(a.ctx, password); err != nil {
		return err
	}
	a.log.Info("vault unlocked")
	a.rememberVault()
	go a.syncVaultQuietly()
	return nil
}

// syncVaultQuietly 在后台执行 bw sync 并重新加载条目。
//
// bw 解锁只读它本地的缓存, 不会自己去服务器拉新数据; 不同步的话, 在 Bitwarden / Vaultwarden
// 里新加的条目在 czlterm 里一直搜不到。解锁先用本地缓存立即可用, 同步完成后条目自动更新。
func (a *App) syncVaultQuietly() {
	if err := a.vault.Refresh(context.Background()); err != nil {
		a.log.Warn("vault background sync", "err", err)
		return
	}
	a.log.Info("vault synced")
}

// rememberVault 在开启「记住解锁」时把当前会话密钥存进系统钥匙串。失败只记日志, 不影响本次解锁。
func (a *App) rememberVault() {
	if !a.settings.Get().VaultRemember {
		return
	}
	if err := secret.Set(secret.VaultSessionKey, a.vault.Session()); err != nil {
		a.log.Warn("remember vault session", "err", err)
	}
}

// restoreVault 启动时用钥匙串里保存的会话自动解锁。会话失效就删掉, 下次照常输主密码。
func (a *App) restoreVault() {
	session, err := secret.Get(secret.VaultSessionKey)
	if err != nil || session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := a.vault.Restore(ctx, session); err != nil {
		a.log.Info("restore vault session", "err", err)
		_ = secret.Set(secret.VaultSessionKey, "")
		return
	}
	a.log.Info("vault restored from keychain")
	wruntime.EventsEmit(a.ctx, eventVaultUnlocked)
	a.syncVaultQuietly()
}

// VaultLock 锁定保险库并关闭用其凭据建立的 agent 端点与连接。
func (a *App) VaultLock() {
	a.vault.Lock()
	a.log.Info("vault locked")
}

// VaultRefresh 从服务器同步条目 (bw sync) 并重新加载。
func (a *App) VaultRefresh() error {
	if err := a.vault.Refresh(a.ctx); err != nil {
		return err
	}
	a.log.Info("vault synced")
	return nil
}

// VaultSearch 搜索可用作凭据的条目 (登录条目与 SSH 密钥条目), 只返回摘要。
func (a *App) VaultSearch(query string) ([]vault.ItemSummary, error) {
	return a.vault.Search(query)
}
