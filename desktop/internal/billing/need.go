package billing

// 同步空间只读时 (同步服务回 40302 或 /spaces 标 readonly), 界面该提示什么。
const (
	// NeedNone 不需要用户为订阅做什么 (可写, 或只读另有原因: 只读成员、已被移出)
	NeedNone = ""
	// NeedSubscribe 没有有效订阅: 引导购买或续费
	NeedSubscribe = "subscribe"
	// NeedSeats 团队有订阅但成员数超过席位: 引导升级套餐或减员
	NeedSeats = "seats"
)

// SpaceNeed 判断一个只读的同步空间缺什么。orgID 为空表示个人空间。
// 应用没开通收款 (enabled 为 false) 时同步免费, 只读与订阅无关。
func SpaceNeed(readOnly bool, orgID string, ent *Entitlements, enabled bool) string {
	if !readOnly || !enabled {
		return NeedNone
	}
	if orgID == "" {
		if ent == nil || ent.Personal == nil || !ent.Personal.Active {
			return NeedSubscribe
		}
		return NeedNone
	}
	if ent == nil {
		return NeedSubscribe
	}
	for _, o := range ent.Orgs {
		if o.OrgID != orgID {
			continue
		}
		if o.Entitlement == nil || !o.Entitlement.Active {
			return NeedSubscribe
		}
		if o.OrgStatus == "readonly" && o.Seats > 0 && o.MemberCount > int64(o.Seats) {
			return NeedSeats
		}
		return NeedNone
	}
	return NeedSubscribe
}

// MaxMore 还能再加购的份数 (上限减已有, 不小于 0)。上限为 0 视为不限, 界面按 99 封顶。
func (a AddOn) MaxMore() int {
	if a.MaxUnits <= 0 {
		return 99
	}
	return max(a.MaxUnits-a.CurrentQuantity, 0)
}

// PlansFor 筛出某类主体能买的在售套餐。
func PlansFor(c *Catalog, subjectType string) []Plan {
	var out []Plan
	if c == nil {
		return out
	}
	for _, p := range c.Plans {
		if p.SubjectType == subjectType && (p.Status == "" || p.Status == "active") {
			out = append(out, p)
		}
	}
	return out
}
