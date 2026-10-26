package executor

import (
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// effectiveCreditModel resolves aliases before consulting per-account pricing.
// Auto, missing prices and zero-cost models must not pretend to spend credits.
func effectiveCreditModel(item Item, publicModel string) string {
	model := routeModel(publicModel)
	if target := item.ModelAliases[model]; target != "" {
		model = routeModel(target)
	}
	return model
}

func routeQuotaExceeded(item Item, q RouteQuery) bool {
	model := q.PublicModel
	if !q.fefoNow.IsZero() {
		model = effectiveCreditModel(item, model)
	}
	if itemServesFreeModel(item, model) {
		return false
	}
	if item.Quota != nil && item.Quota.Exceeded {
		return true
	}
	if !q.fefoNow.IsZero() {
		_, depleted := fefoExpiry(item, q.PublicModel, q.fefoNow)
		return depleted
	}
	return false
}

// fefoExpiry returns a trustworthy, spendable package expiry, not a subscription
// reset or token expiry. Scope is a provider assertion that these packages share
// the metered model's balance AND the upstream deducts earliest-expiring first.
// Mixed/unknown scopes or live packages without dates keep the account in the
// unknown group. Only a complete, scoped package set can prove depletion.
func fefoExpiry(item Item, publicModel string, now time.Time) (expiry int64, depleted bool) {
	model := effectiveCreditModel(item, publicModel)
	if model == "" || itemServesFreeModel(item, model) || item.Quota == nil {
		return 0, false
	}
	metered := false
	for _, id := range item.MeteredModels {
		if CanonicalModelID(id) == model {
			metered = true
			break
		}
	}
	quota := item.Quota
	if !metered || len(quota.Packages) == 0 || quota.HasAddOn || quota.HasResourcePackage {
		return 0, false
	}
	unknown := false
	live := false
	for _, pkg := range quota.Packages {
		if pkg.Scope != accounts.QuotaPackageScopeAccountFEFO || (pkg.Unit != "" && pkg.Unit != "credits") {
			return 0, false
		}
		if pkg.Remain <= 0 || (pkg.EndsAt > 0 && pkg.EndsAt <= now.Unix()) {
			continue
		}
		live = true
		if pkg.EndsAt <= 0 {
			unknown = true
			continue
		}
		if expiry == 0 || pkg.EndsAt < expiry {
			expiry = pkg.EndsAt
		}
	}
	if !live {
		return 0, true
	}
	if unknown {
		return 0, false
	}
	return expiry, false
}

// fefoQuotaTracked reports whether the account's credit state is within the
// provider-confirmed FEFO scope for this model, even if package dates are
// unknown. Such accounts still need post-use refresh so a spent balance can
// become depleted; unknown dates themselves never invent a priority.
func fefoQuotaTracked(item Item, publicModel string) bool {
	model := effectiveCreditModel(item, publicModel)
	if model == "" || itemServesFreeModel(item, model) || item.Quota == nil || len(item.Quota.Packages) == 0 ||
		item.Quota.HasAddOn || item.Quota.HasResourcePackage {
		return false
	}
	metered := false
	for _, id := range item.MeteredModels {
		if CanonicalModelID(id) == model {
			metered = true
			break
		}
	}
	if !metered {
		return false
	}
	for _, pkg := range item.Quota.Packages {
		if pkg.Scope != accounts.QuotaPackageScopeAccountFEFO || (pkg.Unit != "" && pkg.Unit != "credits") {
			return false
		}
	}
	return true
}

// HasFEFOQuota reports whether a completed request could have consumed a
// trustworthy package balance. Runtime uses this gate to refresh only opt-in,
// supported routes; the three historical strategies add no upstream traffic.
func (p *Pool) HasFEFOQuota(accountID, publicModel string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.routingStrategy != RoutingStrategyFEFO {
		return false
	}
	for _, item := range p.items {
		if item.ID == accountID {
			return fefoQuotaTracked(item, publicModel)
		}
	}
	return false
}

// FEFOQuotaDepleted reports whether an account that is routed by FEFO has a
// trustworthy package set with no live positive balance. A naturally expired
// cache looks exactly like this while its top-level Exceeded flag stays false.
// The runtime stale-quota sweep uses it to discover newly granted upstream
// packages; unknown scopes/dates never qualify.
func (p *Pool) FEFOQuotaDepleted(accountID string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.routingStrategy != RoutingStrategyFEFO {
		return false
	}
	for _, item := range p.items {
		if item.ID != accountID {
			continue
		}
		for _, model := range item.MeteredModels {
			if _, depleted := fefoExpiry(item, model, time.Now()); depleted {
				return true
			}
		}
		return false
	}
	return false
}

// fefoCandidates narrows an already healthy/model/region/concurrency-filtered
// pool to the earliest billing date. The existing ID-cursor round-robin then
// distributes that day's requests fairly. No known dates means ordinary RR.
// UTC+8 is the billing calendar for the currently supported credit packages;
// compare actual Unix seconds above before grouping so expired-today is not live.
func fefoCandidates(available []Item, publicModel string, now time.Time) []Item {
	var earliest int64
	out := make([]Item, 0, len(available))
	for _, item := range available {
		expiry, _ := fefoExpiry(item, publicModel, now)
		if expiry == 0 {
			continue
		}
		day := (expiry + 8*60*60) / (24 * 60 * 60)
		if earliest == 0 || day < earliest {
			earliest = day
			out = out[:0]
		}
		if day == earliest {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return available
	}
	return out
}
