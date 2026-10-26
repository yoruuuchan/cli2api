package workbuddy

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestUserResourceFEFOScope(t *testing.T) {
	for _, tc := range []struct {
		name         string
		enterpriseID string
		count        int
		wantScope    string
	}{
		{"personal", "", 1, accounts.QuotaPackageScopeAccountFEFO},
		{"personal-under-page-size", "", 99, accounts.QuotaPackageScopeAccountFEFO},
		{"enterprise-identity-unknown", "enterprise-fixture", 1, ""},
		{"malformed-enterprise-identity", " ", 1, ""},
		{"full-page-incomplete", "", 100, ""},
		{"overflow-incomplete", "", 101, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := make([]resourcePackage, tc.count)
			for i := range raw {
				raw[i] = resourcePackage{ProductCode: "p_tcaca", CapacityUnit: "credits", CycleCapacitySize: 10, CycleCapacityRemain: 10, CycleEndTime: "2026-10-25 00:00:00"}
			}
			client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 0, "data": map[string]any{"Response": map[string]any{"Data": map[string]any{"Accounts": raw}}},
				})
			}))
			remain, used, total, packages, err := client.UserResource(context.Background(), "fixture-account", Credential{
				AccessToken: "fixture-token", UID: "fixture-user", EnterpriseID: tc.enterpriseID, Domain: DomainCN,
			})
			if err != nil {
				t.Fatal(err)
			}
			if remain != int64(tc.count*10) || total != remain || used != 0 || len(packages) != tc.count {
				t.Fatalf("remain=%d used=%d total=%d packages=%d", remain, used, total, len(packages))
			}
			for _, pkg := range packages {
				if pkg.Scope != tc.wantScope || pkg.EndsAt == 0 {
					t.Fatalf("package=%+v want scope=%q and preserved expiry", pkg, tc.wantScope)
				}
			}
		})
	}
}

func TestUserResourceQueryUsesBillingTimezone(t *testing.T) {
	// Do not let the machine's local zone silently change the API's UTC+8
	// wall-clock query range. This package does not run tests in parallel.
	previous := time.Local
	time.Local = time.FixedZone("fixture-UTC-7", -7*60*60)
	t.Cleanup(func() { time.Local = previous })
	before := time.Now().Add(-time.Second)
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Begin string `json:"PackageEndTimeRangeBegin"`
			End   string `json:"PackageEndTimeRangeEnd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		begin, err := time.ParseInLocation("2006-01-02 15:04:05", payload.Begin, cycleEndTimeZone)
		if err != nil {
			t.Error(err)
		}
		end, err := time.ParseInLocation("2006-01-02 15:04:05", payload.End, cycleEndTimeZone)
		if err != nil {
			t.Error(err)
		}
		if begin.Before(before) || begin.After(time.Now()) {
			t.Errorf("query begin=%s outside current instant range; host timezone leaked", begin)
		}
		if end.Sub(begin) != 365*101*24*time.Hour {
			t.Errorf("query range=%s", end.Sub(begin))
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[]}}}}`))
	}))
	if _, _, _, _, err := client.UserResource(context.Background(), "fixture-account", Credential{UID: "fixture-user", AccessToken: "fixture-token"}); err != nil {
		t.Fatal(err)
	}
}

func TestSoonestExpiryAtSkipsUnspendableBalances(t *testing.T) {
	packages := []providers.QuotaPackage{
		{Remain: 50, EndsAt: 0},
		{Remain: 0, EndsAt: 1100},
		{Remain: -5, EndsAt: 1100},
		{Remain: math.NaN(), EndsAt: 1100},
		{Remain: 40, EndsAt: 999},
		{Remain: 40, EndsAt: 1000},
		{Remain: 2, EndsAt: 1200},
		{Remain: 3, EndsAt: 1200},
		{Remain: 10, EndsAt: 1300},
	}
	if expiry, remain := soonestExpiryAt(packages, 1000); expiry != 1200 || remain != 5 {
		t.Fatalf("expiry=%d remain=%v want 1200/5", expiry, remain)
	}
	if expiry, remain := soonestExpiryAt(packages, 1300); expiry != 0 || remain != 0 {
		t.Fatalf("expired snapshot expiry=%d remain=%v want unknown/0", expiry, remain)
	}
}

func TestQuotaPackagesScopeRequiresOrdinaryCreditProduct(t *testing.T) {
	for _, tc := range []struct {
		name, product, unit, wantScope string
	}{
		{"ordinary-credits", "p_tcaca", "credits", accounts.QuotaPackageScopeAccountFEFO},
		{"product-unknown", "", "credits", ""},
		{"unit-unknown", "p_tcaca", "", ""},
		{"other-product", "other-product", "credits", ""},
		{"other-unit", "p_tcaca", "tokens", ""},
		{"all-unknown", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packages := quotaPackages([]resourcePackage{{
				ProductCode: tc.product, CapacityUnit: tc.unit, CycleCapacityRemain: 5, CycleCapacitySize: 5,
			}})
			if len(packages) != 1 || packages[0].Scope != tc.wantScope || packages[0].Unit != tc.unit {
				t.Fatalf("package=%+v want scope=%q unit=%q", packages, tc.wantScope, tc.unit)
			}
		})
	}
}

func TestQuotaPackagesPreciseCredits(t *testing.T) {
	for _, tc := range []struct {
		name, remain, used, size string
		wantRemain, wantUsed     float64
		wantScope                string
	}{
		{"fractional-string", `"0.69"`, `"0.31"`, `"1"`, 0.69, 0.31, accounts.QuotaPackageScopeAccountFEFO},
		{"fractional-number", `0.69`, `0.31`, `1`, 0.69, 0.31, accounts.QuotaPackageScopeAccountFEFO},
		{"fractional-decimal-string", `".69"`, `".31"`, `"1"`, 0.69, 0.31, accounts.QuotaPackageScopeAccountFEFO},
		{"partial-remain", `"0.69"`, `null`, `"1"`, 0.69, 0.31, accounts.QuotaPackageScopeAccountFEFO},
		{"partial-used", `null`, `"0.31"`, `"1"`, 0.69, 0.31, accounts.QuotaPackageScopeAccountFEFO},
		{"malformed", `"not-a-number"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"nan", `"NaN"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"infinity", `"Infinity"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"negative", `"-0.69"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"outside-size", `"1.69"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"inconsistent-sum", `"0.69"`, `"0.69"`, `"1"`, 0, 1, ""},
		{"malformed-size", `"0.69"`, `"0.31"`, `{}`, 0, 1, ""},
		{"null-string-invalid", `"null"`, `"0.31"`, `"1"`, 0, 1, ""},
		{"size-only-unknown-balance", `null`, `null`, `"1"`, 0, 1, ""},
		{"all-unreported", `null`, `""`, `null`, 0, 1, accounts.QuotaPackageScopeAccountFEFO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packages := quotaPackages([]resourcePackage{{
				ProductCode: "p_tcaca", CapacityUnit: "credits", CapacityRemain: 500, CapacitySize: 500,
				CycleCapacitySize: 1, CycleCapacityRemain: 0,
				CycleCapacityRemainPrecise: json.RawMessage(tc.remain),
				CycleCapacityUsedPrecise:   json.RawMessage(tc.used),
				CycleCapacitySizePrecise:   json.RawMessage(tc.size),
			}})
			if len(packages) != 1 || math.Abs(packages[0].Remain-tc.wantRemain) > 1e-12 ||
				math.Abs(packages[0].Used-tc.wantUsed) > 1e-12 || packages[0].Size != 1 || packages[0].Scope != tc.wantScope {
				t.Fatalf("package=%+v want remain=%v used=%v size=1 scope=%q", packages, tc.wantRemain, tc.wantUsed, tc.wantScope)
			}
		})
	}
}

func TestQuotaPackagesCyclePrecedesLifetimePrecision(t *testing.T) {
	pkg := resourcePackage{
		ProductCode: "p_tcaca", CapacityUnit: "credits", CapacityRemain: 500, CapacitySize: 500,
		CapacityRemainPrecise: json.RawMessage(`"500"`), CapacityUsedPrecise: json.RawMessage(`"0"`), CapacitySizePrecise: json.RawMessage(`"500"`),
		CycleCapacitySize: 1, CycleCapacityRemain: 0,
	}
	packages := quotaPackages([]resourcePackage{pkg})
	if packages[0].Remain != 0 || packages[0].Size != 1 {
		t.Fatalf("lifetime precise balance revived exhausted cycle: %+v", packages)
	}
	pkg.CycleCapacitySize = 0
	packages = quotaPackages([]resourcePackage{pkg})
	if packages[0].Remain != 500 || packages[0].Size != 500 {
		t.Fatalf("missing cycle did not use lifetime precision: %+v", packages)
	}
	pkg.CycleCapacityRemainPrecise = json.RawMessage(`"0"`)
	pkg.CycleCapacityUsedPrecise = json.RawMessage(`"1"`)
	pkg.CycleCapacitySizePrecise = json.RawMessage(`"1"`)
	packages = quotaPackages([]resourcePackage{pkg})
	if packages[0].Remain != 0 || packages[0].Size != 1 {
		t.Fatalf("lifetime balance overrode explicitly depleted precise cycle: %+v", packages)
	}
}

func TestQuotaUsesPrecisePackageBalanceForExceeded(t *testing.T) {
	client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{
			"ProductCode":"p_tcaca","CapacityUnit":"credits","CapacityRemain":500,"CapacitySize":500,
			"CycleCapacityRemain":0,"CycleCapacityUsed":0,"CycleCapacitySize":1,
			"CycleCapacityRemainPrecise":"0.69","CycleCapacityUsedPrecise":"0.31","CycleCapacitySizePrecise":"1",
			"CycleEndTime":"2026-10-25 00:00:00"
		}]}}}}`))
	}))
	payload, err := (Credential{AccessToken: "fixture-token", UID: "fixture-user", ExpiresAt: 4102444800, Domain: DomainCN}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCredentialPayload(context.Background(), "fixture-account", CredentialFormat, payload); err != nil {
		t.Fatal(err)
	}
	info, err := client.Quota(context.Background(), "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	if info.Exceeded || math.Abs(info.Remaining-0.69) > 1e-12 || math.Abs(info.Used-0.31) > 1e-12 || info.Total != 1 {
		t.Fatalf("fractional credits were falsely exhausted: %+v", info)
	}
	if math.Abs(info.Percentage-31) > 1e-12 || info.Packages[0].Scope != accounts.QuotaPackageScopeAccountFEFO {
		t.Fatalf("precise quota metadata=%+v", info)
	}
}

func TestQuotaRejectsUnreliablePrecisionBeforeExceeded(t *testing.T) {
	for _, tc := range []struct {
		name, remain, used, size, attributes string
		wantError, wantExceeded              bool
		wantRemain                           float64
		wantScope                            string
	}{
		{"invalid-remain", `"precision-fixture-marker-not-a-number"`, `".31"`, `"1"`, "", true, false, 0, ""},
		{"size-only", `null`, `null`, `"1"`, "", true, false, 0, ""},
		{"inconsistent-amounts", `".69"`, `".69"`, `"1"`, "", true, false, 0, ""},
		{"partial-remain-valid", `".69"`, `null`, `"1"`, "", false, false, 0.69, accounts.QuotaPackageScopeAccountFEFO},
		{"partial-used-valid", `null`, `".31"`, `"1"`, "", false, false, 0.69, accounts.QuotaPackageScopeAccountFEFO},
		{"unknown-attributes-not-invalid-precision", `".69"`, `".31"`, `"1"`, `[{"Key":"modelScope","Type":5,"Value":"model-only"}]`, false, false, 0.69, ""},
		{"legacy-integer-zero-unknown-scope", `null`, `null`, `null`, `[{"Key":"modelScope","Type":5,"Value":"model-only"}]`, false, true, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := resourcePackage{
				ProductCode: "p_tcaca", CapacityUnit: "credits", CycleCapacitySize: 1,
				CycleCapacityRemainPrecise: json.RawMessage(tc.remain),
				CycleCapacityUsedPrecise:   json.RawMessage(tc.used), CycleCapacitySizePrecise: json.RawMessage(tc.size),
			}
			if tc.attributes != "" {
				pkg.AccountAttributes = json.RawMessage(tc.attributes)
			}
			body, err := json.Marshal(map[string]any{
				"code": 0, "data": map[string]any{"Response": map[string]any{"Data": map[string]any{"Accounts": []resourcePackage{pkg}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			client, store := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(body)
			}))
			payload, err := (Credential{AccessToken: "fixture-token", UID: "fixture-user", ExpiresAt: 4102444800, Domain: DomainCN}).Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SaveCredentialPayload(context.Background(), "fixture-account", CredentialFormat, payload); err != nil {
				t.Fatal(err)
			}
			info, err := client.Quota(context.Background(), "fixture-account")
			if tc.wantError {
				if err == nil || info != nil {
					t.Fatalf("unreliable precision produced a quota snapshot: info=%+v err=%v", info, err)
				}
				if err.Error() != "user-resource quota has invalid or incomplete precise amounts" {
					t.Fatalf("precision failure must return only the fixed sanitized diagnostic")
				}
				return
			}
			if err != nil || info == nil {
				t.Fatalf("valid amounts rejected: info=%+v err=%v", info, err)
			}
			if info.Exceeded != tc.wantExceeded || math.Abs(info.Remaining-tc.wantRemain) > 1e-12 || info.Packages[0].Scope != tc.wantScope {
				t.Fatalf("quota=%+v want exceeded=%v remain=%v scope=%q", info, tc.wantExceeded, tc.wantRemain, tc.wantScope)
			}
		})
	}
}

func TestQuotaPackagesRejectDeductionRestrictions(t *testing.T) {
	for _, tc := range []struct {
		properties string
		allowed    bool
	}{
		{"", true}, {"null", true}, {"[]", true}, {"[ ]", true},
		{`[{"model":"fixture-model"}]`, false}, {`{}`, false}, {`"unknown"`, false},
	} {
		t.Run(tc.properties, func(t *testing.T) {
			packages := quotaPackages([]resourcePackage{{
				ProductCode: "p_tcaca", CapacityUnit: "credits", CycleCapacitySize: 1, CycleCapacityRemain: 1,
				DeductionProperties: json.RawMessage(tc.properties),
			}})
			if got := packages[0].Scope == accounts.QuotaPackageScopeAccountFEFO; got != tc.allowed {
				t.Fatalf("properties=%s scope=%q", tc.properties, packages[0].Scope)
			}
		})
	}
}

func TestQuotaPackagesAllowOnlyKnownPersonalAccountAttributes(t *testing.T) {
	for _, tc := range []struct {
		name, attributes string
		allowed          bool
	}{
		{"absent", "", true},
		{"null", "null", true},
		{"empty", "[]", true},
		{"personal-cn", `[{"Key":"grantSource","Type":3,"Value":"synthetic-grant"},{"Key":"payerType","Type":5,"Value":"0"},{"Key":"payerUin","Type":5,"Value":"synthetic-payer-identifier"}]`, true},
		{"personal-global", `[{"Key":"payerType","Type":5,"Value":"0"},{"Key":"payerUin","Type":5,"Value":"synthetic-payer-identifier"}]`, true},
		{"unknown-model-restriction", `[{"Key":"modelScope","Type":5,"Value":"model-only"}]`, false},
		{"shared-payer", `[{"Key":"payerType","Type":5,"Value":"1"}]`, false},
		{"numeric-payer-unverified", `[{"Key":"payerType","Type":5,"Value":0}]`, false},
		{"unknown-type", `[{"Key":"payerUin","Type":9,"Value":"synthetic-payer-identifier"}]`, false},
		{"missing-value", `[{"Key":"payerUin","Type":5}]`, false},
		{"extra-field", `[{"Key":"payerType","Type":5,"Value":"0","Model":"model-only"}]`, false},
		{"wrong-object-shape", `{}`, false},
		{"wrong-array-shape", `["payerType"]`, false},
		{"null-array-entry", `[null]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packages := quotaPackages([]resourcePackage{{
				ProductCode: "p_tcaca", CapacityUnit: "credits", CycleCapacitySize: 1, CycleCapacityRemain: 1,
				AccountAttributes: json.RawMessage(tc.attributes),
			}})
			if got := packages[0].Scope == accounts.QuotaPackageScopeAccountFEFO; got != tc.allowed {
				t.Fatalf("scope=%q want allowed=%v", packages[0].Scope, tc.allowed)
			}
			encoded, err := json.Marshal(packages)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "synthetic-payer-identifier") || strings.Contains(string(encoded), "payerUin") ||
				strings.Contains(string(encoded), "grantSource") {
				t.Fatalf("transient account metadata leaked into quota package JSON")
			}
		})
	}
}

func TestQuotaPackagesUnparseableExpiryStaysUnknown(t *testing.T) {
	packages := quotaPackages([]resourcePackage{{ProductCode: "p_tcaca", CapacityUnit: "credits", CycleCapacitySize: 5, CycleCapacityRemain: 5, CycleEndTime: "not-a-date"}})
	if len(packages) != 1 || packages[0].EndsAt != 0 || packages[0].Scope != accounts.QuotaPackageScopeAccountFEFO {
		t.Fatalf("package=%+v", packages)
	}
}
