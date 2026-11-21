// Package billing 是 CZL Connect 收款与权益 (/api/oauth2/billing, 用户令牌) 的客户端。
//
// czlterm 的付费功能是云同步: 个人同步 (personal) 与团队版 (team / team-plus)。应用开通收款后,
// 同步服务按订阅放行写入, 没有有效订阅的空间只读 (403 40302); 这里只负责查目录与权益、预览价格、
// 拼托管结账页地址 (付款一律在系统浏览器里的 Connect 结账页完成), 拦截以同步服务为准。
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// DefaultBaseURL 是正式环境的收款接口根地址。
const DefaultBaseURL = "https://connect.czl.net/api/oauth2/billing"

// CheckoutBase 是托管结账页的根地址: <CheckoutBase>/<应用 ID>/<套餐 code>?subject=...
const CheckoutBase = "https://connect.czl.net/pay"

// 套餐 code (在 Connect 应用后台「收款 → 套餐」建好, 不能改)。
const (
	PlanPersonal = "personal"
	PlanTeam     = "team"
	PlanTeamPlus = "team-plus"
)

// 主体类型。
const (
	SubjectUser = "user"
	SubjectOrg  = "org"
)

// App 是目录里的应用信息, ID 用于托管结账页地址。
type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Plan 是在售套餐。
type Plan struct {
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	PriceCents  int64          `json:"priceCents"`
	Currency    string         `json:"currency"`
	PeriodUnit  string         `json:"periodUnit"`
	PeriodCount int            `json:"periodCount"`
	SubjectType string         `json:"subjectType"`
	Seats       int            `json:"seats"`
	Quotas      map[string]any `json:"quotas"`
	TrialDays   int            `json:"trialDays"`
	Status      string         `json:"status"`
	SortOrder   int            `json:"sortOrder"`
	// MultiPeriods 可一次购买的多个周期 (如 2 年 / 3 年) 与折扣, 按周期数升序; 空 = 只能按 1 个周期买
	MultiPeriods []PlanTerm `json:"multiPeriods"`
}

// PlanTerm 一次购买 Periods 个周期, 套餐价少付 PercentOff%。
type PlanTerm struct {
	Periods    int `json:"periods"`
	PercentOff int `json:"percentOff"`
}

// 加购项作用的权益: TargetSeats 加到席位上, 其余是套餐 quotas 的键 (如 syncStorageGB)。
const (
	TargetSeats   = "seats"
	TargetStorage = "syncStorageGB"
)

// AddOn 是某个主体当前套餐可加购的一项 (目录按 ?subject= 列出; 主体没有生效中的订阅时为空)。
// 买在周期中途按剩余天数折算 (ProratedUnitCents), 立即生效、与套餐同一天到期, 续费时按份数一起续。
type AddOn struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	// PriceCents 每份单价 (套餐一个周期)
	PriceCents int64 `json:"priceCents"`
	// ProratedUnitCents 本期每份折算价 (按剩余天数), 本次应付 = 它 × 份数
	ProratedUnitCents int64 `json:"proratedUnitCents"`
	// Target 作用到权益的哪一项: TargetSeats 或 quotas 的键
	Target string `json:"target"`
	// UnitAmount 每份加多少 (1 席位、10 GB)
	UnitAmount float64 `json:"unitAmount"`
	// MaxUnits 最多加购的份数 (含已有的)
	MaxUnits int `json:"maxUnits"`
	// CurrentQuantity 当前已有的份数
	CurrentQuantity int        `json:"currentQuantity"`
	PeriodEnd       *time.Time `json:"periodEnd"`
	AutoRenew       bool       `json:"autoRenew"`
	// Remaining 还能再加的份数, 由 Catalog 按 MaxMore 填好 (Connect 不下发)
	Remaining int `json:"remaining"`
}

// EntitlementAddOn 是订阅上已加购的一项。Entitlement 的 Seats / Quotas 已是套餐与加购的合计, 这里是明细。
type EntitlementAddOn struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Target     string  `json:"target"`
	UnitAmount float64 `json:"unitAmount"`
	Quantity   int     `json:"quantity"`
	// Amount 份数 × 每份数量
	Amount float64 `json:"amount"`
	// PendingQuantity 下个周期的份数 (调低后到期才生效), nil 为没有调整
	PendingQuantity    *int       `json:"pendingQuantity"`
	PendingEffectiveAt *time.Time `json:"pendingEffectiveAt"`
}

// Catalog 是在售的套餐、加购项与可用支付方式。
type Catalog struct {
	App     App      `json:"app"`
	Plans   []Plan   `json:"plans"`
	AddOns  []AddOn  `json:"addons"`
	Methods []string `json:"methods"`
}

// Entitlement 是一个主体 (本人或组织) 的订阅权益。Active 为 false 时 (过期、退款) 同步只读。
type Entitlement struct {
	Active            bool           `json:"active"`
	Plan              string         `json:"plan"`
	PlanName          string         `json:"planName"`
	Status            string         `json:"status"`
	PeriodStart       time.Time      `json:"periodStart"`
	PeriodEnd         time.Time      `json:"periodEnd"`
	GraceUntil        *time.Time     `json:"graceUntil"`
	CancelAtPeriodEnd bool           `json:"cancelAtPeriodEnd"`
	PendingPlan       string         `json:"pendingPlan,omitempty"`
	Seats             int            `json:"seats"`
	Quotas            map[string]any `json:"quotas"`
	// AddOns 已加购的明细 (Seats / Quotas 已含它们)
	AddOns []EntitlementAddOn `json:"addons"`
}

// OrgEntitlement 是本人所在组织的订阅。Entitlement 为 nil 表示从没订阅过。
type OrgEntitlement struct {
	OrgID       string       `json:"orgId"`
	Name        string       `json:"name"`
	Role        string       `json:"role"`
	OrgStatus   string       `json:"orgStatus"`
	Seats       int          `json:"seats"`
	MemberCount int64        `json:"memberCount"`
	Entitlement *Entitlement `json:"entitlement"`
}

// Entitlements 是本人与所属各组织的权益。
type Entitlements struct {
	Personal *Entitlement     `json:"personal"`
	Orgs     []OrgEntitlement `json:"orgs"`
}

// TokenSource 提供用户访问令牌 (由 internal/account 实现)。
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// Client 是收款接口客户端。并发安全。
type Client struct {
	base   string
	tokens TokenSource
	hc     *http.Client
}

// New 创建客户端。base 为空用正式地址; hc 为空用 15 秒超时的默认客户端。
func New(base string, tokens TokenSource, hc *http.Client) *Client {
	if base == "" {
		base = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), tokens: tokens, hc: hc}
}

// Catalog 返回在售套餐, 以及 subject (SubjectUser 或 "org:<组织 id>", 空为本人) 当前套餐可加购的项。
// 应用没开通收款时返回 ErrBillingDisabled。
func (c *Client) Catalog(ctx context.Context, subject string) (*Catalog, error) {
	var out Catalog
	path := "/catalog"
	if subject != "" {
		path += "?" + url.Values{"subject": {subject}}.Encode()
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.Plans == nil {
		out.Plans = []Plan{}
	}
	if out.AddOns == nil {
		out.AddOns = []AddOn{}
	}
	for i := range out.Plans {
		if out.Plans[i].MultiPeriods == nil {
			out.Plans[i].MultiPeriods = []PlanTerm{}
		}
	}
	for i := range out.AddOns {
		out.AddOns[i].Remaining = out.AddOns[i].MaxMore()
	}
	return &out, nil
}

// Entitlements 返回本人与所属各组织的权益。应用没开通收款时照常返回 (都为空)。
func (c *Client) Entitlements(ctx context.Context) (*Entitlements, error) {
	var out Entitlements
	if err := c.do(ctx, http.MethodGet, "/entitlements", nil, &out); err != nil {
		return nil, err
	}
	if out.Orgs == nil {
		out.Orgs = []OrgEntitlement{}
	}
	return &out, nil
}

// planPreviewBody 套餐预览的请求体; 单期不带 periods (升级补差价不接受该字段)。
func planPreviewBody(plan, subject string, periods int) map[string]any {
	body := map[string]any{"plan": plan, "subject": subject}
	if periods > 1 {
		body["periods"] = periods
	}
	return body
}

// SetAddOnQuantity 调低加购项在下个周期的份数 (当前周期结束后生效); 传当前份数为撤回调低。
// 增加份数在托管结账页付款 (AddOnCheckoutURL)。返回调整后的权益。
func (c *Client) SetAddOnQuantity(ctx context.Context, subject, addon string, quantity int) (*Entitlement, error) {
	var out Entitlement
	body := map[string]any{"subject": subject, "addon": addon, "quantity": quantity}
	return &out, c.do(ctx, http.MethodPost, "/addon-quantity", body, &out)
}

// CheckoutURL 是套餐 (开通、续费、升级、降级) 的托管结账页地址, 在系统浏览器里打开。个人不带 subject,
// periods > 1 为一次买多个周期, returnURL 是付完款「返回应用」的地址 (空则不带)。优惠券在结账页里填, 不带。
func CheckoutURL(appID, plan, subject string, periods int, returnURL string) string {
	u := CheckoutBase + "/" + url.PathEscape(appID) + "/" + url.PathEscape(plan)
	q := url.Values{}
	if returnURL != "" {
		q.Set("return", returnURL)
	}
	if strings.HasPrefix(subject, SubjectOrg+":") {
		q.Set("subject", subject)
	}
	if periods > 1 {
		q.Set("periods", fmt.Sprint(periods))
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// AddOnCheckoutURL 是加购项 (加席位、加容量) 的托管结账页地址, 份数预填为 quantity。
func AddOnCheckoutURL(appID, addon, subject string, quantity int, returnURL string) string {
	q := url.Values{"addon": {addon}, "qty": {fmt.Sprint(quantity)}, "subject": {subject}}
	if returnURL != "" {
		q.Set("return", returnURL)
	}
	return CheckoutBase + "/" + url.PathEscape(appID) + "?" + q.Encode()
}

// codePeriodsUnsupported 是 Connect 在套餐没开放这个周期数时随 400 返回的数字码。
const codePeriodsUnsupported = 40028

// OrgSubject 是组织主体的写法。
func OrgSubject(orgID string) string { return SubjectOrg + ":" + orgID }

// do 发请求并按状态码归类错误。接口错误体为 {"error": "人话"}, 只进日志, 界面按码出文案。
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	tok, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return errcode.ErrPayloadCodec.Wrap(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errcode.ErrBillingRequest.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return errcode.ErrBillingRequest.Wrap(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errcode.ErrBillingRequest.Wrap(err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
			Code  int    `json:"code"`
		}
		_ = json.Unmarshal(raw, &e)
		cause := fmt.Errorf("%s %s: http %d %s", method, path, resp.StatusCode, e.Error)
		if resp.StatusCode == http.StatusBadRequest {
			if e.Code == codePeriodsUnsupported {
				return errcode.ErrBillingPeriods.Wrap(cause)
			}
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return errcode.ErrAuthExpired.Wrap(cause)
		case http.StatusForbidden:
			// 应用没开通收款, 或为组织购买但不是 owner / admin
			if strings.HasPrefix(path, "/catalog") {
				return errcode.ErrBillingDisabled.Wrap(cause)
			}
			return errcode.ErrBillingForbidden.Wrap(cause)
		case http.StatusBadRequest, http.StatusNotFound:
			return errcode.ErrBillingRejected.Wrap(cause)
		default:
			return errcode.ErrBillingRequest.Wrap(cause)
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errcode.ErrBillingRequest.Wrap(errors.Join(errors.New("decode response"), err))
	}
	return nil
}
