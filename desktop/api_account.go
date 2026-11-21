package main

// CZL Connect 登录绑定。登录是可选的: 只给云同步、团队与订阅用, 不登录时本机功能全部照常。
//
// 登录结果经事件推给前端: 授权在系统浏览器里完成, 回调在 Windows 上由第二个进程转来、
// 在 macOS 上由系统 URL 事件送达, 都与发起登录的那次前端调用不在同一个调用链上。

import (
	"context"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/account"
	"github.com/woodchen-ink/czlterm/desktop/internal/deeplink"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/settings"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

// 前端订阅的登录与链接事件。
const (
	eventAuthChanged = "auth:changed" // 载荷 AccountStatus
	eventAuthFailed  = "auth:failed"  // 载荷 错误串 "数字码 英文短语"
	// eventTeamJoin 收到加入团队的链接, 载荷邀请码; 前端弹确认框, 用户确认才加入
	eventTeamJoin = "team:join"
)

// AccountStatus 是给界面的登录状态。
type AccountStatus struct {
	account.Status
	// Configured 为 false 时本版本没配 Connect 客户端 id, 登录入口显示「暂未开通」
	Configured bool `json:"configured"`
}

// accountState 是登录相关的运行时状态。
type accountState struct {
	// pendingJoin 是经链接送来、还没处理的加入团队邀请码 (冷启动与未登录时先记着)
	pendingJoin atomic.Value
}

// AccountStatus 返回当前登录状态, 必要时联网续期; 已登录时顺带启动云同步。
func (a *App) AccountStatus() (AccountStatus, error) {
	if err := a.ready(); err != nil {
		return AccountStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	st, err := a.account.Status(ctx)
	if err != nil {
		a.log.Warn("account status", "err", err)
	}
	if st.LoggedIn && st.User != nil {
		a.signedIn(st.User)
	}
	return AccountStatus{Status: st, Configured: a.account.Configured()}, err
}

// Login 在系统浏览器打开授权页, 并返回该地址供用户复制到别的浏览器打开;
// 结果经 auth:changed / auth:failed 事件送达。回调走系统登记的协议, 哪个浏览器都能唤回。
func (a *App) Login() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	u, err := a.account.Begin()
	if err != nil {
		return "", err
	}
	wruntime.BrowserOpenURL(a.ctx, u)
	return u, nil
}

// CancelLogin 放弃进行中的登录。
func (a *App) CancelLogin() {
	if a.ready() == nil {
		a.account.Cancel()
	}
}

// Logout 退出登录并删掉本机的端到端加密密钥 (再登录要恢复码或已登录设备批准)。
// 同步空间里还有能推上去的修改时, force 为 false 返回 6108 由界面确认: 修改留在本机,
// 用同一账号重新登录并解锁后继续上传。
func (a *App) Logout(force bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !force && a.cloud.UnpushedCount() > 0 {
		return errcode.ErrAuthLogoutUnpushed
	}
	if err := a.account.Logout(); err != nil {
		a.log.Warn("account logout", "err", err)
		return err
	}
	a.signedOut(true)
	return nil
}

// signedIn 在登录成功或启动时确认已登录后调用: 启动该账号的云同步并刷新订阅。
func (a *App) signedIn(u *account.User) {
	if u == nil || u.ID == "" {
		return
	}
	if err := a.cloud.Start(string(u.ID)); err != nil {
		a.log.Warn("cloud start", "err", err)
	}
	go a.BillingRefresh(true)
}

// onAuthExpired 在后台续期被拒、登录记录已清掉时调用: 停同步, 界面回到未登录。
// 本机密钥保留: 只是令牌失效, 重新登录同一账号后不用再输恢复码。
func (a *App) onAuthExpired() {
	if a.ctx == nil {
		return
	}
	a.signedOut(false)
}

// signedOut 是退出登录后的收尾: 停同步、清订阅缓存、回到本机空间、通知界面。
func (a *App) signedOut(forgetKeys bool) {
	a.cloud.Stop(forgetKeys)
	a.resetBilling()
	if a.settings.Get().Space != space.Local {
		if _, err := a.settings.Update(func(s *settings.Settings) { s.Space = space.Local }); err != nil {
			a.log.Warn("reset space", "err", err)
		}
		wruntime.EventsEmit(a.ctx, eventSpaceChanged, space.Local)
	}
	wruntime.EventsEmit(a.ctx, eventAuthChanged, AccountStatus{Configured: a.account.Configured()})
}

// handleLink 按链接类型分发, 认得返回 true。Windows 第二实例与 macOS URL 事件都走这里。
//   - 登录回调: 走登录流程
//   - 加入团队: 记下邀请码并通知前端, 已登录时弹确认框 (不自动加入), 未登录则登录后再取
//   - 结账回跳: 通知前端立即查一次权益 (只当信号, 不信任链接参数)
func (a *App) handleLink(raw string) bool {
	l, ok := deeplink.Parse(raw)
	if !ok {
		return false
	}
	if a.ctx != nil {
		a.showWindow()
	}
	switch l.Kind {
	case deeplink.KindOAuth:
		// 启动还没完成 (或失败) 时登录管理器可能还不在; 冷启动本来也没有进行中的登录
		if a.ctx != nil && a.ready() == nil {
			go a.completeLogin(raw)
		}
	case deeplink.KindTeamJoin:
		a.accountState.pendingJoin.Store(l.Code)
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, eventTeamJoin, l.Code)
		}
	case deeplink.KindBillingDone:
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, eventBillingDone)
		}
	}
	return true
}

// TakePendingJoin 取走待处理的加入团队邀请码, 没有返回空串。前端登录完成后调用。
func (a *App) TakePendingJoin() string {
	if v, ok := a.accountState.pendingJoin.Swap("").(string); ok {
		return v
	}
	return ""
}

func (a *App) completeLogin(callback string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user, err := a.account.Complete(ctx, callback)
	if err != nil {
		a.log.Warn("account complete", "err", err)
		wruntime.EventsEmit(a.ctx, eventAuthFailed, err.Error())
		return
	}
	a.signedIn(user)
	wruntime.EventsEmit(a.ctx, eventAuthChanged, AccountStatus{
		Status: account.Status{LoggedIn: true, User: user}, Configured: true,
	})
}
