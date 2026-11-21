package main

// 订阅与购买绑定。云同步是付费功能: 个人同步 (personal)、团队版 (team / team-plus)。
// 应用开通收款后, 没有有效订阅的同步空间被同步服务设为只读 (40302); 这里查权益、预览多年价格,
// 购买 / 续费 / 升级 / 加购一律在系统浏览器打开 Connect 托管结账页 (支付方式、优惠券由结账页提供),
// 打开前记下权益指纹, 界面定时调 BillingCheckPaid 对比, 变了即付款到账。应用没开通收款时同步免费, 购买入口不显示。

import (
	"context"
	"errors"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/billing"
	"github.com/woodchen-ink/czlterm/desktop/internal/deeplink"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// eventBillingChanged 订阅状态变了 (刷新、付款成功), 载荷 BillingState。
const eventBillingChanged = "billing:changed"

// eventBillingDone 收到结账页回跳 czlterm://billing/done: 界面立即查一次权益 (等待付款的弹窗按指纹判断到账)。
const eventBillingDone = "billing:done"

// BillingState 是给界面的订阅状态。
type BillingState struct {
	// Enabled 应用开通了收款 (没开通时同步免费, 不显示购买入口)
	Enabled bool `json:"enabled"`
	// Loaded 至少成功取到过一次 (没取到时界面不下结论)
	Loaded bool           `json:"loaded"`
	AppID  string         `json:"appId"`
	Plans  []billing.Plan `json:"plans"`
	// AddOns 本人 (个人同步) 当前套餐可加购的项; 团队的按需用 BillingAddOns 取
	AddOns       []billing.AddOn       `json:"addons"`
	Methods      []string              `json:"methods"`
	Entitlements *billing.Entitlements `json:"entitlements"`
	// Error 最近一次刷新失败的错误串, 成功为空
	Error string `json:"error"`
}

// billingCache 缓存最近一次的订阅状态, 空间列表按它判断「需要订阅」。
// gen 在退出登录时加一: 进行中的刷新带着旧代次回来就丢掉, 不把上一个账号的权益写回缓存。
type billingCache struct {
	mu    sync.Mutex
	state BillingState
	at    time.Time
	gen   int
}

// resetBilling 在退出登录时清掉缓存并让进行中的刷新作废。
func (a *App) resetBilling() {
	a.billingCache.mu.Lock()
	a.billingCache.state, a.billingCache.at = BillingState{}, time.Time{}
	a.billingCache.gen++
	a.billingCache.mu.Unlock()
}

func (c *billingCache) get() BillingState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// BillingState 返回缓存的订阅状态 (不联网)。
func (a *App) BillingState() BillingState {
	return a.billingCache.get()
}

// BillingRefresh 联网刷新订阅状态。回到窗口时前端调用, 30 秒内重复调用直接返回缓存。
func (a *App) BillingRefresh(force bool) BillingState {
	a.billingCache.mu.Lock()
	fresh := !force && a.billingCache.state.Loaded && time.Since(a.billingCache.at) < 30*time.Second
	gen := a.billingCache.gen
	a.billingCache.mu.Unlock()
	if fresh {
		return a.BillingState()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	next := a.BillingState()
	next.Error = ""
	cat, err := a.billing.Catalog(ctx, billing.SubjectUser)
	switch {
	case err == nil:
		next.Enabled, next.AppID, next.Plans, next.AddOns, next.Methods = true, cat.App.ID, cat.Plans, cat.AddOns, cat.Methods
	case errors.Is(err, errcode.ErrBillingDisabled):
		next.Enabled, next.Plans, next.AddOns, next.Methods = false, nil, nil, nil
	default:
		a.logErr("billing.catalog", err)
		next.Error = err.Error()
	}
	if err == nil || errors.Is(err, errcode.ErrBillingDisabled) {
		if ent, err := a.billing.Entitlements(ctx); err != nil {
			a.logErr("billing.entitlements", err)
			next.Error = err.Error()
		} else {
			next.Entitlements, next.Loaded = ent, true
		}
	}
	a.billingCache.mu.Lock()
	if a.billingCache.gen != gen {
		a.billingCache.mu.Unlock()
		return BillingState{} // 刷新途中退出了登录
	}
	a.billingCache.state, a.billingCache.at = next, time.Now()
	a.billingCache.mu.Unlock()
	// 界面收到后重读空间列表 (「需要订阅」标记随之更新), 不发 space:changed: 空间没换, 不该清掉在编辑提示
	wruntime.EventsEmit(a.ctx, eventBillingChanged, next)
	return next
}

// BillingQuotePlan 返回主体 (subject 为 "user" 或 "org:<组织 id>") 购买套餐的各个年限与价格 (取 Connect 下单预览)。
// 套餐没开放多周期、或这次是升级补差价时只有 1 个选项。
func (a *App) BillingQuotePlan(plan, subject string) (*billing.PlanQuote, error) {
	var p *billing.Plan
	for _, it := range a.BillingState().Plans {
		if it.Code == plan {
			p = &it
			break
		}
	}
	if p == nil {
		return nil, errcode.ErrBillingRejected
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	q, err := a.billing.QuotePlan(ctx, *p, subject)
	if err != nil {
		a.logErr("billing.quote", err)
		return nil, err
	}
	return q, nil
}

// BillingAddOns 返回主体 (subject 为 "user" 或 "org:<组织 id>") 当前套餐可加购的项, 带已有份数与本期折算价。
// 主体没有生效中的订阅时为空 (加购须先有套餐)。
func (a *App) BillingAddOns(subject string) ([]billing.AddOn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cat, err := a.billing.Catalog(ctx, subject)
	if err != nil {
		a.logErr("billing.addons", err)
		return nil, err
	}
	return cat.AddOns, nil
}

// BillingSetAddOnQuantity 调低加购项在下个周期的份数 (当前周期结束后生效); 传当前份数为撤回调低。
// 增加份数走 BillingOpenAddOnCheckout。成功后刷新订阅状态。
func (a *App) BillingSetAddOnQuantity(subject, addon string, quantity int) error {
	if quantity < 0 {
		return errcode.ErrBillingRejected
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.billing.SetAddOnQuantity(ctx, subject, addon, quantity); err != nil {
		a.logErr("billing.addon_quantity", err)
		return err
	}
	go a.BillingRefresh(true)
	return nil
}

// BillingOpenCheckout 在系统浏览器打开套餐 (开通、续费、升级、降级) 的托管结账页, 一次买 periods 个周期。
// 返回打开前的权益指纹, 界面带着它调 BillingCheckPaid 等付款到账。
func (a *App) BillingOpenCheckout(plan, subject string, periods int) (string, error) {
	st := a.BillingState()
	if st.AppID == "" {
		return "", errcode.ErrBillingDisabled
	}
	if periods < 1 {
		periods = 1
	}
	fp := a.billingFingerprint(subject)
	wruntime.BrowserOpenURL(a.ctx, billing.CheckoutURL(st.AppID, plan, subject, periods, deeplink.BillingDoneURL))
	return fp, nil
}

// BillingOpenAddOnCheckout 在系统浏览器打开加购项的托管结账页 (份数预填), 返回打开前的权益指纹。
func (a *App) BillingOpenAddOnCheckout(addon, subject string, quantity int) (string, error) {
	st := a.BillingState()
	if st.AppID == "" {
		return "", errcode.ErrBillingDisabled
	}
	if quantity < 1 {
		return "", errcode.ErrBillingRejected
	}
	fp := a.billingFingerprint(subject)
	wruntime.BrowserOpenURL(a.ctx, billing.AddOnCheckoutURL(st.AppID, addon, subject, quantity, deeplink.BillingDoneURL))
	return fp, nil
}

// billingFingerprint 联网取一次权益算指纹 (缓存可能是几十秒前的); 取不到时退回缓存里的。
func (a *App) billingFingerprint(subject string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ent, err := a.billing.Entitlements(ctx)
	if err != nil {
		a.logErr("billing.entitlements", err)
		ent = a.BillingState().Entitlements
	}
	return billing.Fingerprint(ent, subject)
}

// BillingCheckPaid 重取权益, 与打开结账页前的指纹 before 对比; 变了即付款到账, 随即刷新订阅与同步空间
// (只读空间恢复可写) 并返回 true。界面在等待付款时每隔几秒、以及回到窗口时调用。
func (a *App) BillingCheckPaid(subject, before string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ent, err := a.billing.Entitlements(ctx)
	if err != nil {
		a.logErr("billing.check_paid", err)
		return false, err
	}
	if billing.Fingerprint(ent, subject) == before {
		return false, nil
	}
	a.BillingRefresh(true)
	go a.CloudRefresh()
	return true, nil
}
