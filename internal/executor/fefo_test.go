package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func fefoTime(days, hour int) int64 {
	now := time.Now().In(time.FixedZone("billing", 8*60*60))
	return time.Date(now.Year(), now.Month(), now.Day()+days, hour, 0, 0, 0, now.Location()).Unix()
}

func fefoPack(remain float64, expiry int64) accounts.QuotaPackage {
	return accounts.QuotaPackage{Remain: remain, Size: remain, EndsAt: expiry, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO}
}

func fefoItem(id string, packages ...accounts.QuotaPackage) Item {
	return Item{ID: id, Provider: "workbuddy", Region: "cn", Models: []string{"paid", "free", "unknown"},
		MeteredModels: []string{"paid"}, FreeModels: []string{"free"},
		Quota: &QuotaSnapshot{Total: 100, Remaining: 100, Unit: "credits", Packages: packages}}
}

func fefoPool(items ...Item) *Pool {
	p := NewPool(nil, nil)
	p.SetRoutingStrategy(RoutingStrategyFEFO)
	for _, item := range items {
		p.Upsert(item)
	}
	return p
}

func assertFEFOPick(t *testing.T, p *Pool, q RouteQuery, want string) {
	t.Helper()
	item, ok := p.PickRoute(q)
	if !ok || item.ID != want {
		t.Fatalf("pick = %q, %v; want %q", item.ID, ok, want)
	}
}

func TestFEFOSelectsSpendablePackagesNotSummary(t *testing.T) {
	later := fefoItem("a", fefoPack(20, fefoTime(3, 12)))
	earlier := fefoItem("z", fefoPack(0, fefoTime(1, 12)), fefoPack(100, time.Now().Add(-time.Hour).Unix()), fefoPack(10, fefoTime(2, 12)))
	later.Quota.ExpiresAt = fefoTime(1, 12) // a stale/aggregate field is not a package.
	p := fefoPool(later, earlier)
	for i := 0; i < 4; i++ {
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
	}
	// A quota refresh changes the winner without replacing/restarting the pool.
	p.MergeQuota("z", &QuotaSnapshot{Packages: []accounts.QuotaPackage{fefoPack(0, fefoTime(2, 12)), fefoPack(20, fefoTime(4, 12))}})
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "a")
}

func TestFEFOSameBillingDayRotatesDespiteDifferentTimesAndWeights(t *testing.T) {
	a := fefoItem("a", fefoPack(5, fefoTime(2, 1)))
	b := fefoItem("b", fefoPack(20, fefoTime(2, 23)))
	a.Weight, b.Weight = 99, 1
	p := fefoPool(a, b, fefoItem("later", fefoPack(100, fefoTime(3, 1))))
	for i := 0; i < 10; i++ {
		want := "a"
		if i%2 == 1 {
			want = "b"
		}
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, want)
	}
}

func TestFEFOUnknownExpiryUsesRoundRobinWithoutInventingDates(t *testing.T) {
	a := fefoItem("a", fefoPack(10, 0))
	b := fefoItem("b")
	b.Quota.ExpiresAt = fefoTime(1, 12)
	p := fefoPool(a, b)
	for i := 0; i < 6; i++ {
		want := "a"
		if i%2 == 1 {
			want = "b"
		}
		assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, want)
	}
}

func TestFEFOUnknownScopesAndMixedDatesRemainUnknown(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Item)
	}{
		{"unknown scope", func(i *Item) { i.Quota.Packages[0].Scope = "" }},
		{"model-restricted bucket", func(i *Item) { i.Quota.Packages[0].Scope = "work-only" }},
		{"different units", func(i *Item) { i.Quota.Packages[0].Unit = "tokens" }},
		{"mixed unknown date", func(i *Item) { i.Quota.Packages = append(i.Quota.Packages, fefoPack(10, 0)) }},
		{"additional unscoped bucket", func(i *Item) { i.Quota.HasAddOn = true }},
		{"unavailable organization resource", func(i *Item) { i.Quota.HasResourcePackage = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fefoItem("a", fefoPack(10, fefoTime(1, 12)))
			test.edit(&a)
			p := fefoPool(a, fefoItem("z", fefoPack(20, fefoTime(2, 12))))
			assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
		})
	}
}

func TestFEFOFreeAutoUnknownPricingAndAliases(t *testing.T) {
	for _, model := range []string{"free", "auto", "unknown", "free-alias"} {
		t.Run(model, func(t *testing.T) {
			a := fefoItem("a", fefoPack(10, fefoTime(3, 12)))
			z := fefoItem("z", fefoPack(10, fefoTime(1, 12)))
			a.ModelAliases = map[string]string{"free-alias": "free"}
			z.ModelAliases = map[string]string{"free-alias": "free"}
			p := fefoPool(a, z)
			assertFEFOPick(t, p, RouteQuery{PublicModel: model}, "a")
			assertFEFOPick(t, p, RouteQuery{PublicModel: model}, "z")
		})
	}
	a := fefoItem("a", fefoPack(10, fefoTime(3, 12)))
	z := fefoItem("z", fefoPack(10, fefoTime(1, 12)))
	a.ModelAliases, z.ModelAliases = map[string]string{"alias": "paid"}, map[string]string{"alias": "paid"}
	p := fefoPool(a, z)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "alias"}, "z")
	// Free aliases keep the existing exhausted-account exemption under FEFO.
	a.ModelAliases = map[string]string{"alias": "free"}
	a.Quota.Exceeded = true
	p = fefoPool(a)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "alias"}, "a")
}

func TestFEFOHonorsEligibilityBeforeExpiry(t *testing.T) {
	falseValue := false
	for _, test := range []struct {
		name  string
		edit  func(*Item)
		query RouteQuery
	}{
		{name: "not ready", edit: func(i *Item) { i.Ready = &falseValue }},
		{name: "model mismatch", edit: func(i *Item) { i.Models = []string{"other"} }},
		{name: "model disabled", edit: func(i *Item) { i.ModelsRestricted = true; i.EnabledModels = []string{"other"} }},
		{name: "quota exhausted", edit: func(i *Item) { i.Quota.Exceeded = true }},
		{name: "zero-balance packages", edit: func(i *Item) { i.Quota.Packages[0].Remain = 0 }},
		{name: "expired since refresh", edit: func(i *Item) { i.Quota.Packages[0].EndsAt = time.Now().Add(-time.Second).Unix() }},
		{name: "concurrency", edit: func(i *Item) { i.MaxInFlight = 1; i.InFlight = 1 }},
		{name: "account cooldown", edit: func(i *Item) { i.DownUntil = time.Now().Add(time.Hour) }},
		{name: "model cooldown", edit: func(i *Item) { i.ModelDownUntil = map[string]time.Time{"paid": time.Now().Add(time.Hour)} }},
		{name: "region restriction", edit: func(i *Item) { i.Region = "global" }, query: RouteQuery{RegionFilter: "cn"}},
		{name: "key region restriction", edit: func(i *Item) { i.Region = "global" }, query: RouteQuery{AllowedProviders: []string{"workbuddy:cn"}}},
		{name: "provider restriction", edit: func(i *Item) { i.Provider = "qoder" }, query: RouteQuery{ProviderFilter: "workbuddy"}},
		{name: "retry exclusion", query: RouteQuery{Excluded: map[string]struct{}{"a": {}}}},
		{name: "protocol eligibility", query: RouteQuery{Eligible: func(i Item) bool { return i.ID != "a" }}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fefoItem("a", fefoPack(10, fefoTime(1, 12)))
			if test.edit != nil {
				test.edit(&a)
			}
			p := fefoPool(a, fefoItem("z", fefoPack(10, fefoTime(2, 12))))
			q := test.query
			q.PublicModel = "paid"
			assertFEFOPick(t, p, q, "z")
		})
	}
}

func TestFEFODepletedHintAndSaturation(t *testing.T) {
	p := fefoPool(fefoItem("a", fefoPack(0, fefoTime(1, 12))))
	q := RouteQuery{PublicModel: "paid"}
	if _, ok := p.PickRoute(q); ok || p.LenRoute(q) != 0 {
		t.Fatal("depleted snapshot must not route")
	}
	if _, ok := p.QuotaBlockedForModel(q); !ok {
		t.Fatal("depleted snapshot must report quota, not model mismatch")
	}
	assertFEFOPick(t, p, RouteQuery{PublicModel: "free"}, "a")
	p = fefoPool(fefoItem("a", fefoPack(10, fefoTime(1, 12))))
	p.Upsert(Item{ID: "a", Provider: "workbuddy", Region: "cn", MaxInFlight: 1})
	if !p.Acquire("a") {
		t.Fatal("acquire")
	}
	if _, ok := p.PickRoute(q); ok {
		t.Fatal("saturated account must not route")
	}
	p.Release("a")
	assertFEFOPick(t, p, q, "a")
}

func TestFEFOCatalogRefreshClearsEligibilityAndPreservesRuntimeUpserts(t *testing.T) {
	p := fefoPool(fefoItem("a", fefoPack(10, fefoTime(3, 12))), fefoItem("z", fefoPack(10, fefoTime(1, 12))))
	p.Upsert(Item{ID: "z", Provider: "workbuddy", Region: "cn"})
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "z")
	p.MergeModelCatalog("z", []string{"paid"}, nil, nil)
	assertFEFOPick(t, p, RouteQuery{PublicModel: "paid"}, "a")
	p.MergeModelCatalog("z", []string{"paid"}, []string{"paid"}, []string{"paid"})
	item, _ := p.ByID("z")
	if len(item.MeteredModels) != 0 {
		t.Fatal("free declaration wins over metered")
	}
}

func TestFEFOLeavesOriginalStrategiesUnchanged(t *testing.T) {
	for _, strategy := range []string{RoutingStrategyRoundRobin, RoutingStrategyWeightedRoundRobin, RoutingStrategyFillFirst} {
		t.Run(strategy, func(t *testing.T) {
			a := fefoItem("a", fefoPack(0, fefoTime(1, 12)))
			z := fefoItem("z", fefoPack(20, fefoTime(2, 12)))
			a.Weight, z.Weight = 90, 10
			p := fefoPool(a, z)
			p.SetRoutingStrategy(strategy)
			counts := map[string]int{}
			for i := 0; i < 10; i++ {
				item, ok := p.PickRoute(RouteQuery{PublicModel: "paid"})
				if !ok {
					t.Fatal("old strategy gate changed")
				}
				counts[item.ID]++
			}
			want := 5
			if strategy == RoutingStrategyWeightedRoundRobin {
				want = 9
			}
			if strategy == RoutingStrategyFillFirst {
				want = 10
			}
			if counts["a"] != want {
				t.Fatalf("counts=%v", counts)
			}
		})
	}
}

func TestFEFOChatAffinityPinAndFailureEscape(t *testing.T) {
	var fail atomic.Bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.Header().Set("X-Qoder-Error-Kind", "rate_limit")
			w.Header().Set("X-Qoder-Failover", "1")
			w.Header().Set("Retry-After", "30")
			http.Error(w, `{"error":{"kind":"rate_limit","message":"busy"}}`, http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"model":"paid","choices":[{"message":{"content":"a"},"finish_reason":"stop"}]}`)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"model":"paid","choices":[{"message":{"content":"b"},"finish_reason":"stop"}]}`)
	}))
	defer b.Close()
	ia, ib := fefoItem("a", fefoPack(10, fefoTime(2, 12))), fefoItem("b", fefoPack(10, fefoTime(4, 12)))
	ia.Provider, ib.Provider = "qoder", "qoder" // isolated fake child runtimes exercise the shared executor.
	ia.Region, ib.Region = "global", "global"
	ia.URL, ib.URL = a.URL, b.URL
	ia.Runtime, ib.Runtime = "child_process", "child_process"
	p := fefoPool(ia, ib)
	ex := NewChatExecutor(p, "")
	ctx := WithSessionKey(context.Background(), "fefo-session")
	req := translate.ChatRequest{Model: "paid", Messages: []translate.ChatMessage{{Role: "user", Content: "FEFO test"}}}
	first, err := ex.ChatNonStream(ctx, req, "", "")
	if err != nil || first.AccountID != "a" || first.Routing != routingPool {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	p.MergeQuota("b", &QuotaSnapshot{Packages: []accounts.QuotaPackage{fefoPack(10, fefoTime(1, 12))}})
	sticky, err := ex.ChatNonStream(ctx, req, "", "")
	if err != nil || sticky.AccountID != "a" || sticky.Routing != routingSticky {
		t.Fatalf("sticky=%+v err=%v", sticky, err)
	}
	pinned, err := ex.ChatNonStream(ctx, req, "b", "")
	if err != nil || pinned.AccountID != "b" || pinned.Routing != routingPin {
		t.Fatalf("pin=%+v err=%v", pinned, err)
	}
	fail.Store(true)
	escaped, err := ex.ChatNonStream(ctx, req, "", "")
	if err != nil || escaped.AccountID != "b" || escaped.Routing != routingStickyEscape || escaped.AttemptCount != 2 {
		t.Fatalf("escape=%+v err=%v", escaped, err)
	}
}

func TestFEFOQuotaTrackingGateCoversOnlyConfirmedScopes(t *testing.T) {
	unknownDate := fefoItem("unknown-date", fefoPack(10, 0))
	if !fefoPool(unknownDate).HasFEFOQuota("unknown-date", "paid") {
		t.Fatal("confirmed scope with unknown date still needs post-use refresh")
	}
	unknownScope := fefoItem("unknown-scope", fefoPack(10, fefoTime(1, 12)))
	unknownScope.Quota.Packages[0].Scope = ""
	if fefoPool(unknownScope).HasFEFOQuota("unknown-scope", "paid") {
		t.Fatal("unknown scope must not create quota traffic")
	}
	free := fefoItem("free", fefoPack(10, fefoTime(1, 12)))
	if fefoPool(free).HasFEFOQuota("free", "free") {
		t.Fatal("free model does not spend credits")
	}
	if fefoPool(free).HasFEFOQuota("free", "unpriced") {
		t.Fatal("unknown pricing is not metered")
	}
	check := fefoPool(free)
	check.SetRoutingStrategy(RoutingStrategyFillFirst)
	if check.HasFEFOQuota("free", "paid") {
		t.Fatal("other strategies must not create FEFO quota traffic")
	}
	expired := fefoItem("expired", fefoPack(100, fefoTime(-1, 12)))
	if !fefoPool(expired).FEFOQuotaDepleted("expired") {
		t.Fatal("expired trusted package should be revalidated in FEFO")
	}
}
