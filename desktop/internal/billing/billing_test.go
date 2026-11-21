package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

type tok string

func (t tok) AccessToken(context.Context) (string, error) { return string(t), nil }

// fakeBilling 按 Connect 的收款接口契约: 目录、权益。
func fakeBilling(t *testing.T, enabled bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/catalog" && !enabled:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"该应用未开通收款"}`))
		case r.URL.Path == "/catalog":
			w.Write([]byte(`{"app":{"id":"app_pub_1","name":"CZL 单证生成器"},"methods":["alipay_qr","alipay_page"],"plans":[
				{"code":"personal","name":"个人同步","priceCents":3400,"currency":"CNY","periodUnit":"year","periodCount":1,"subjectType":"user","status":"active"},
				{"code":"team","name":"团队版","priceCents":14900,"currency":"CNY","periodUnit":"year","periodCount":1,"subjectType":"org","seats":5,"status":"active"},
				{"code":"team-plus","name":"团队版 Plus","priceCents":44900,"currency":"CNY","periodUnit":"year","periodCount":1,"subjectType":"org","seats":20,"status":"active"}]}`))
		case r.URL.Path == "/entitlements":
			w.Write([]byte(`{"personal":null,"orgs":[{"orgId":"o1","name":"T","role":"owner","orgStatus":"active","seats":5,"memberCount":2,"entitlement":null}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogAndEntitlements(t *testing.T) {
	srv := fakeBilling(t, true)
	c := New(srv.URL, tok("t"), nil)
	ctx := context.Background()

	cat, err := c.Catalog(ctx, "")
	if err != nil || cat.App.ID != "app_pub_1" || len(PlansFor(cat, SubjectOrg)) != 2 || len(PlansFor(cat, SubjectUser)) != 1 {
		t.Fatalf("catalog = %+v, %v", cat, err)
	}
	ent, err := c.Entitlements(ctx)
	if err != nil || ent.Personal != nil || len(ent.Orgs) != 1 {
		t.Fatalf("entitlements = %+v, %v", ent, err)
	}

}

// 应用没开通收款: 目录回 403 认作「未开通」, 不当成故障。
func TestBillingDisabled(t *testing.T) {
	srv := fakeBilling(t, false)
	_, err := New(srv.URL, tok("t"), nil).Catalog(context.Background(), SubjectUser)
	if !errors.Is(err, errcode.ErrBillingDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckoutURL(t *testing.T) {
	if got := CheckoutURL("app_pub_1", PlanPersonal, SubjectUser, 1, ""); got != "https://connect.czl.net/pay/app_pub_1/personal" {
		t.Fatalf("personal = %s", got)
	}
	got := CheckoutURL("app_pub_1", PlanTeam, OrgSubject("o1"), 1, "czlterm://billing/done")
	if !strings.HasPrefix(got, "https://connect.czl.net/pay/app_pub_1/team?") || !strings.Contains(got, "subject=org%3Ao1") || !strings.Contains(got, "return=czlterm%3A%2F%2Fbilling%2Fdone") {
		t.Fatalf("team = %s", got)
	}
}

func TestSpaceNeed(t *testing.T) {
	active := &Entitlement{Active: true, PeriodEnd: time.Now().Add(time.Hour)}
	ent := &Entitlements{
		Personal: &Entitlement{Active: false},
		Orgs: []OrgEntitlement{
			{OrgID: "paid", Entitlement: active, OrgStatus: "active", Seats: 5, MemberCount: 3},
			{OrgID: "full", Entitlement: active, OrgStatus: "readonly", Seats: 5, MemberCount: 7},
			{OrgID: "unpaid"},
		},
	}
	cases := []struct {
		ro      bool
		org     string
		enabled bool
		want    string
	}{
		{false, "", true, NeedNone},
		{true, "", false, NeedNone}, // 没开通收款: 只读与订阅无关
		{true, "", true, NeedSubscribe},
		{true, "unpaid", true, NeedSubscribe},
		{true, "full", true, NeedSeats},
		{true, "paid", true, NeedNone},
		{true, "unknown", true, NeedSubscribe},
	}
	for _, c := range cases {
		if got := SpaceNeed(c.ro, c.org, ent, c.enabled); got != c.want {
			t.Errorf("SpaceNeed(%v, %q, enabled=%v) = %q, want %q", c.ro, c.org, c.enabled, got, c.want)
		}
	}
}

// 加购项 (按 Connect 线上契约): 目录按主体列出可加购项与折算价, 权益带加购明细, 调低份数, 结账页地址。
func TestAddOns(t *testing.T) {
	var gotSubject string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/catalog":
			gotSubject = r.URL.Query().Get("subject")
			w.Write([]byte(`{"app":{"id":"app_pub_1"},"plans":[],"addons":[
				{"id":"a1","code":"seat","name":"加 1 席位","priceCents":3000,"currency":"CNY","unitAmount":1,"target":"seats","maxUnits":50,
				 "currentQuantity":2,"proratedUnitCents":1500,"periodEnd":"2027-05-20T00:00:00Z","autoRenew":false},
				{"id":"a2","code":"storage10","name":"加 10 GB 存储","priceCents":1000,"currency":"CNY","unitAmount":10,"target":"syncStorageGB","maxUnits":100,
				 "currentQuantity":0,"proratedUnitCents":500}]}`))
		case r.URL.Path == "/entitlements":
			w.Write([]byte(`{"personal":{"active":true,"plan":"personal","seats":0,"quotas":{"syncStorageGB":25},"addons":[
				{"code":"storage10","name":"加 10 GB 存储","target":"syncStorageGB","unitAmount":10,"quantity":2,"amount":20,"pendingQuantity":1,"pendingEffectiveAt":"2027-11-20T10:00:00Z"}]},"orgs":[]}`))
		case r.URL.Path == "/addon-quantity":
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`{"active":true,"plan":"team","seats":6,"addons":[{"code":"seat","quantity":2,"pendingQuantity":1}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, tok("t"), nil)
	ctx := context.Background()

	cat, err := c.Catalog(ctx, OrgSubject("o1"))
	if err != nil || gotSubject != "org:o1" || len(cat.AddOns) != 2 {
		t.Fatalf("catalog = %+v, %v (subject %q)", cat, err, gotSubject)
	}
	seat := cat.AddOns[0]
	if seat.Target != TargetSeats || seat.ProratedUnitCents != 1500 || seat.CurrentQuantity != 2 || seat.PeriodEnd == nil || seat.MaxMore() != 48 || seat.Remaining != 48 {
		t.Fatalf("seat = %+v", seat)
	}
	if (AddOn{MaxUnits: 3, CurrentQuantity: 5}).MaxMore() != 0 || (AddOn{}).MaxMore() != 99 {
		t.Fatal("MaxMore bounds")
	}

	ent, err := c.Entitlements(ctx)
	if err != nil || len(ent.Personal.AddOns) != 1 {
		t.Fatalf("entitlements = %+v, %v", ent, err)
	}
	if a := ent.Personal.AddOns[0]; a.Quantity != 2 || a.PendingQuantity == nil || *a.PendingQuantity != 1 || a.PendingEffectiveAt == nil {
		t.Fatalf("addon detail = %+v", a)
	}

	e, err := c.SetAddOnQuantity(ctx, OrgSubject("o1"), "seat", 1)
	if err != nil || body["addon"] != "seat" || body["quantity"] != float64(1) || body["subject"] != "org:o1" || e.Seats != 6 {
		t.Fatalf("set quantity = %+v %v body=%v", e, err, body)
	}

	u := AddOnCheckoutURL("app_pub_1", "seat", OrgSubject("o1"), 3, "czlterm://billing/done")
	if !strings.HasPrefix(u, "https://connect.czl.net/pay/app_pub_1?") || !strings.Contains(u, "addon=seat") ||
		!strings.Contains(u, "qty=3") || !strings.Contains(u, "subject=org%3Ao1") || !strings.Contains(u, "return=czlterm%3A%2F%2Fbilling%2Fdone") {
		t.Fatalf("checkout = %s", u)
	}
}
