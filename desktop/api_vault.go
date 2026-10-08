package main

import (
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
	return nil
}

// VaultLock 锁定保险库并关闭用其凭据建立的 agent 端点与连接。
func (a *App) VaultLock() {
	a.vault.Lock()
	a.log.Info("vault locked")
}

// VaultRefresh 从服务器同步条目。
func (a *App) VaultRefresh() error {
	return a.vault.Refresh(a.ctx)
}

// VaultSearch 搜索可用作凭据的条目 (登录条目与 SSH 密钥条目), 只返回摘要。
func (a *App) VaultSearch(query string) ([]vault.ItemSummary, error) {
	return a.vault.Search(query)
}
