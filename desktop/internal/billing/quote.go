package billing

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// 套餐的购买年限与价格。价格一律取 Connect 的下单预览 (order-preview), 与下单同一套计算, 客户端不自己算折扣:
// 续费时应付还含加购份数, 升级是补差价, 这些只有服务端知道。

// Preview 是 Connect 下单预览的结果。OriginalCents 为优惠券之前 (已含多周期折扣), AmountCents 为应付。
type Preview struct {
	Kind          string `json:"kind"`
	ItemCode      string `json:"itemCode"`
	Quantity      int    `json:"quantity"`
	OriginalCents int64  `json:"originalCents"`
	DiscountCents int64  `json:"discountCents"`
	AmountCents   int64  `json:"amountCents"`
	Currency      string `json:"currency"`
}

// 预览与订单的 kind: 开通 / 续费 / 期末降级都是 plan, 升级补差价是 upgrade (只能按 1 个周期)。
const (
	KindPlan    = "plan"
	KindUpgrade = "upgrade"
)

// PreviewPlanOrder 预览为主体一次买 periods 个周期套餐的应付, 不建订单。
func (c *Client) PreviewPlanOrder(ctx context.Context, plan, subject string, periods int) (*Preview, error) {
	var out Preview
	return &out, c.do(ctx, http.MethodPost, "/order-preview", planPreviewBody(plan, subject, periods), &out)
}

// PlanOption 是一个购买年限的价格。
type PlanOption struct {
	Periods    int `json:"periods"`
	PercentOff int `json:"percentOff"`
	// ListCents 按单期价格买这么多期的合计 (单期应付 × 周期数), 即折扣前的原价
	ListCents int64 `json:"listCents"`
	// AmountCents 实付
	AmountCents int64 `json:"amountCents"`
	// SavedCents 比逐期购买少付的金额
	SavedCents int64 `json:"savedCents"`
}

// PlanQuote 是主体购买某套餐的各个年限与价格。Options 第一项是 1 个周期, 其余按周期数升序。
type PlanQuote struct {
	Plan     string       `json:"plan"`
	Kind     string       `json:"kind"`
	Currency string       `json:"currency"`
	Options  []PlanOption `json:"options"`
}

// QuotePlan 按套餐开放的年限逐个预览价格。升级补差价只有 1 个周期; 某个年限被服务端拒绝 (配置刚改) 时跳过它。
func (c *Client) QuotePlan(ctx context.Context, plan Plan, subject string) (*PlanQuote, error) {
	one, err := c.PreviewPlanOrder(ctx, plan.Code, subject, 1)
	if err != nil {
		return nil, err
	}
	q := &PlanQuote{Plan: plan.Code, Kind: one.Kind, Currency: one.Currency,
		Options: []PlanOption{{Periods: 1, ListCents: one.OriginalCents, AmountCents: one.AmountCents}}}
	if one.Kind != KindPlan {
		return q, nil
	}
	terms := make([]PlanTerm, 0, len(plan.MultiPeriods))
	for _, t := range plan.MultiPeriods {
		if t.Periods > 1 {
			terms = append(terms, t)
		}
	}
	opts := make([]*PlanOption, len(terms))
	errs := make([]error, len(terms))
	var wg sync.WaitGroup
	for i, t := range terms {
		wg.Add(1)
		go func(i int, t PlanTerm) {
			defer wg.Done()
			p, err := c.PreviewPlanOrder(ctx, plan.Code, subject, t.Periods)
			if err != nil {
				errs[i] = err
				return
			}
			list := one.OriginalCents * int64(t.Periods)
			opts[i] = &PlanOption{Periods: t.Periods, PercentOff: t.PercentOff, ListCents: list, AmountCents: p.AmountCents,
				SavedCents: max(list-p.AmountCents, 0)}
		}(i, t)
	}
	wg.Wait()
	for i, o := range opts {
		if o != nil {
			q.Options = append(q.Options, *o)
			continue
		}
		if !errors.Is(errs[i], errcode.ErrBillingPeriods) {
			return nil, errs[i]
		}
	}
	sort.SliceStable(q.Options, func(i, j int) bool { return q.Options[i].Periods < q.Options[j].Periods })
	return q, nil
}
