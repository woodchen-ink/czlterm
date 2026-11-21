package billing

import (
	"encoding/json"
	"strings"
	"time"
)

// 付款在系统浏览器里的托管结账页完成, 客户端拿不到订单号, 只能对比权益: 打开结账页前记一份指纹,
// 之后定时重取权益, 指纹变了 (开通、续费顺延、升级、期末降级、加购) 即视为付款已到账。

// Fingerprint 返回主体 (SubjectUser 或 "org:<组织 id>") 权益里付款会改变的部分拼成的串。
// 主体没有订阅时也有确定的值, 开通后随之改变。
func Fingerprint(ent *Entitlements, subject string) string {
	var e *Entitlement
	seats := 0
	if ent != nil {
		if orgID, ok := strings.CutPrefix(subject, SubjectOrg+":"); ok {
			for _, o := range ent.Orgs {
				if o.OrgID == orgID {
					e, seats = o.Entitlement, o.Seats
					break
				}
			}
		} else {
			e = ent.Personal
		}
	}
	type addOn struct {
		Code     string `json:"c"`
		Quantity int    `json:"q"`
	}
	fp := struct {
		Active      bool           `json:"a"`
		Plan        string         `json:"p"`
		PendingPlan string         `json:"pp"`
		PeriodEnd   time.Time      `json:"e"`
		Seats       int            `json:"s"`
		OrgSeats    int            `json:"os"`
		Quotas      map[string]any `json:"q"`
		AddOns      []addOn        `json:"ao"`
	}{OrgSeats: seats}
	if e != nil {
		fp.Active, fp.Plan, fp.PendingPlan, fp.PeriodEnd, fp.Seats, fp.Quotas = e.Active, e.Plan, e.PendingPlan, e.PeriodEnd.UTC(), e.Seats, e.Quotas
		for _, a := range e.AddOns {
			fp.AddOns = append(fp.AddOns, addOn{a.Code, a.Quantity})
		}
	}
	b, _ := json.Marshal(fp) // map 的键按字典序输出, 结果稳定
	return string(b)
}
