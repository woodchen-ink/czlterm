package billing

import (
	"testing"
	"time"
)

// 付款到账判断: 开通、续费顺延、升级、加购都会改变指纹; 别的主体变化不影响; 同样的权益指纹稳定。
func TestFingerprint(t *testing.T) {
	end := time.Date(2027, 11, 20, 0, 0, 0, 0, time.UTC)
	ent := func() *Entitlements {
		return &Entitlements{
			Personal: &Entitlement{Active: true, Plan: PlanPersonal, PeriodEnd: end, Quotas: map[string]any{"syncStorageGB": 5.0, "b": 1.0}},
			Orgs: []OrgEntitlement{
				{OrgID: "o1", Seats: 5, Entitlement: &Entitlement{Active: true, Plan: PlanTeam, PeriodEnd: end, Seats: 5}},
				{OrgID: "o2"},
			},
		}
	}
	base := ent()
	user, org, none := Fingerprint(base, SubjectUser), Fingerprint(base, OrgSubject("o1")), Fingerprint(base, OrgSubject("o2"))
	if Fingerprint(ent(), SubjectUser) != user || user == org {
		t.Fatal("fingerprint not stable / not per subject")
	}

	renewed := ent()
	renewed.Personal.PeriodEnd = end.AddDate(3, 0, 0)
	if Fingerprint(renewed, SubjectUser) == user || Fingerprint(renewed, OrgSubject("o1")) != org {
		t.Fatal("renewal")
	}
	upgraded := ent()
	upgraded.Orgs[0].Entitlement.Plan, upgraded.Orgs[0].Seats = PlanTeamPlus, 20
	if Fingerprint(upgraded, OrgSubject("o1")) == org {
		t.Fatal("upgrade")
	}
	seat := ent()
	seat.Orgs[0].Entitlement.AddOns = []EntitlementAddOn{{Code: "seat", Quantity: 1}}
	if Fingerprint(seat, OrgSubject("o1")) == org {
		t.Fatal("add-on")
	}
	opened := ent()
	opened.Orgs[1].Entitlement = &Entitlement{Active: true, Plan: PlanTeam, PeriodEnd: end}
	if Fingerprint(opened, OrgSubject("o2")) == none {
		t.Fatal("first subscription")
	}
	if Fingerprint(nil, SubjectUser) == "" {
		t.Fatal("nil entitlements")
	}
}
