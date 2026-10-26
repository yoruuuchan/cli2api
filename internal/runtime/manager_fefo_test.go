package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

type fefoCatalogFixture struct {
	models []providers.ModelInfo
	err    error
}

func (f *fefoCatalogFixture) Models(context.Context, string) ([]providers.ModelInfo, error) {
	return f.models, f.err
}

func fefoModelContains(models []string, want string) bool {
	for _, model := range models {
		if accounts.CanonicalModelID(model) == accounts.CanonicalModelID(want) {
			return true
		}
	}
	return false
}

func TestFetchProviderModelsPropagatesMeteringDeclarations(t *testing.T) {
	catalog := &fefoCatalogFixture{models: []providers.ModelInfo{
		{PublicModel: "paid-public", NativeModel: "Paid_Native", DisplayName: "Shared Label", Metered: true},
		{PublicModel: "free-public", NativeModel: "Free_Native", DisplayName: "Shared Label", Free: true},
		{PublicModel: "conflicting", NativeModel: "conflicting-native", DisplayName: "Conflicting Label", Free: true, Metered: true},
		{PublicModel: "unknown-public", NativeModel: "unknown-native", DisplayName: "Unknown Label"},
	}}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Models: catalog})
	manager := &Manager{pool: NewPool(nil, nil), providers: registry}
	item := Item{ID: "catalog-fixture", Provider: "workbuddy", Runtime: "in_process"}
	manager.pool.Upsert(item)
	manager.fetchProviderModels(context.Background(), item)
	got, _ := manager.pool.ByID(item.ID)
	for _, model := range []string{"paid-public", "paid-native", "shared label", "free-public", "free-native", "conflicting", "unknown-public", "unknown-native", "unknown label"} {
		if !fefoModelContains(got.Models, model) {
			t.Errorf("catalog is missing %q: %v", model, got.Models)
		}
	}
	for _, model := range []string{"paid-public", "paid-native"} {
		if !fefoModelContains(got.MeteredModels, model) {
			t.Errorf("metered list is missing %q: %v", model, got.MeteredModels)
		}
	}
	for _, model := range []string{"free-public", "free-native", "shared label", "conflicting", "conflicting-native", "conflicting label"} {
		if !fefoModelContains(got.FreeModels, model) || fefoModelContains(got.MeteredModels, model) {
			t.Errorf("free must win for %q: free=%v metered=%v", model, got.FreeModels, got.MeteredModels)
		}
	}
	for _, model := range []string{"unknown-public", "unknown-native", "unknown label"} {
		if fefoModelContains(got.FreeModels, model) || fefoModelContains(got.MeteredModels, model) {
			t.Errorf("unknown model %q gained a pricing declaration", model)
		}
	}

	catalog.err = errors.New("synthetic catalog outage")
	manager.fetchProviderModels(context.Background(), item)
	preserved, _ := manager.pool.ByID(item.ID)
	if !fefoModelContains(preserved.MeteredModels, "paid-native") {
		t.Fatal("catalog failure erased the last successful pricing snapshot")
	}
	catalog.err = nil
	catalog.models = []providers.ModelInfo{{PublicModel: "paid-public", NativeModel: "Paid_Native"}}
	manager.fetchProviderModels(context.Background(), item)
	refreshed, _ := manager.pool.ByID(item.ID)
	if len(refreshed.FreeModels) != 0 || len(refreshed.MeteredModels) != 0 {
		t.Fatalf("new unknown pricing must clear stale declarations: %+v", refreshed)
	}
}

type fefoQuotaFixture struct{ info *providers.QuotaInfo }

func (f fefoQuotaFixture) Probe(context.Context, string) (providers.AccountHealth, error) {
	return providers.AccountHealth{Ready: true}, nil
}

func (f fefoQuotaFixture) Quota(context.Context, string) (*providers.QuotaInfo, error) {
	return f.info, nil
}

func TestFetchProviderQuotaPreservesPackageScopeOnlyForWorkBuddy(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "quota-fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "scope-fixture", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{pool: NewPool(nil, nil), store: store}
	manager.pool.Upsert(Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	for _, providerID := range []string{"workbuddy", "trae", ""} {
		t.Run(providerID, func(t *testing.T) {
			info := &providers.QuotaInfo{
				ProviderID: providerID, Remaining: 10, Total: 10, FetchedAt: "2026-10-24T00:00:00Z",
				ExpiresAt: 1792857600, ExpiringRemain: 10,
				Packages: []providers.QuotaPackage{
					{Remain: 10, Size: 10, Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: 1792857600, EndTime: "2026-10-25 00:00:00"},
					{Remain: 5, Size: 5, Unit: "credits", Scope: "work-only"},
					{Remain: 5, Size: 5},
				},
			}
			manager.fetchProviderQuota(ctx, account.ID, fefoQuotaFixture{info: info})
			item, _ := manager.pool.ByID(account.ID)
			stored, err := store.Get(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, quota := range []*accounts.QuotaSnapshot{item.Quota, stored.Quota} {
				if quota == nil {
					t.Fatal("missing quota snapshot")
				}
				if providerID != "workbuddy" {
					if len(quota.Packages) != 0 || quota.ExpiresAt != 0 || quota.ExpiringRemain != 0 {
						t.Fatalf("non-WorkBuddy package metadata leaked through gate: %+v", quota)
					}
					continue
				}
				if len(quota.Packages) != 3 || quota.Packages[0].Scope != accounts.QuotaPackageScopeAccountFEFO ||
					quota.Packages[1].Scope != "work-only" || quota.Packages[2].Scope != "" ||
					quota.Packages[0].Unit != "credits" || quota.Packages[0].EndTime != "2026-10-25 00:00:00" ||
					quota.ExpiresAt != info.ExpiresAt || quota.ExpiringRemain != 10 {
					t.Fatalf("package facts were not preserved: %+v", quota)
				}
			}
		})
	}
}
