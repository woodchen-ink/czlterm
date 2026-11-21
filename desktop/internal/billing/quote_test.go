package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 多周期 (按 Connect 线上契约): 目录带 multiPeriods, 预览按周期数算价 (续费含加购), 未开放的周期数 (40028) 跳过; 结账页带 periods。
func TestQuotePlan(t *testing.T) {
	upgrade := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&in)
		}
		periods := 1
		if v, ok := in["periods"].(float64); ok {
			periods = int(v)
		}
		switch r.URL.Path {
		case "/catalog":
			w.Write([]byte(`{"app":{"id":"app_pub_1"},"plans":[
				{"code":"personal","priceCents":3400,"subjectType":"user","multiPeriods":[{"periods":2,"percentOff":10},{"periods":3,"percentOff":20},{"periods":5,"percentOff":30}]},
				{"code":"team","priceCents":14900,"subjectType":"org"}]}`))
		case "/order-preview":
			if upgrade {
				if periods > 1 {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error":"升级不能多周期"}`))
					return
				}
				w.Write([]byte(`{"kind":"upgrade","quantity":1,"originalCents":5000,"amountCents":5000,"currency":"CNY"}`))
				return
			}
			pct := map[int]int64{1: 0, 2: 10, 3: 20}
			off, ok := pct[periods]
			if !ok { // 5 年在目录里有, 但服务端刚关掉
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"该套餐不支持一次购买这个周期数","code":40028}`))
				return
			}
			// 续费: 套餐 3400 + 加购 1000, 加购按原价 × 周期数
			plan := int64(3400) * int64(periods)
			amt := plan - plan*off/100 + 1000*int64(periods)
			json.NewEncoder(w).Encode(map[string]any{"kind": "plan", "quantity": periods, "originalCents": amt, "amountCents": amt, "currency": "CNY"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, tok("t"), nil)
	ctx := context.Background()

	cat, err := c.Catalog(ctx, SubjectUser)
	if err != nil || len(cat.Plans[0].MultiPeriods) != 3 || cat.Plans[1].MultiPeriods == nil {
		t.Fatalf("catalog = %+v, %v", cat, err)
	}

	q, err := c.QuotePlan(ctx, cat.Plans[0], SubjectUser)
	if err != nil || q.Kind != KindPlan || len(q.Options) != 3 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	if o := q.Options[0]; o.Periods != 1 || o.AmountCents != 4400 || o.ListCents != 4400 || o.SavedCents != 0 {
		t.Fatalf("1 period = %+v", o)
	}
	// 3 年: 原价 4400 × 3 = 13200, 实付 3400×3×0.8 + 1000×3 = 11160
	if o := q.Options[2]; o.Periods != 3 || o.PercentOff != 20 || o.ListCents != 13200 || o.AmountCents != 11160 || o.SavedCents != 2040 {
		t.Fatalf("3 periods = %+v", o)
	}

	upgrade = true
	q, err = c.QuotePlan(ctx, cat.Plans[0], SubjectUser)
	if err != nil || q.Kind != KindUpgrade || len(q.Options) != 1 || q.Options[0].AmountCents != 5000 {
		t.Fatalf("upgrade quote = %+v, %v", q, err)
	}

	u := CheckoutURL("app_pub_1", PlanTeam, OrgSubject("o1"), 3, "")
	if !strings.Contains(u, "periods=3") || !strings.Contains(u, "subject=org%3Ao1") {
		t.Fatalf("checkout = %s", u)
	}
	if u := CheckoutURL("app_pub_1", PlanPersonal, SubjectUser, 2, ""); u != "https://connect.czl.net/pay/app_pub_1/personal?periods=2" {
		t.Fatalf("checkout = %s", u)
	}
}
