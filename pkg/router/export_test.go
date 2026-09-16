package router

import (
	"time"

	"amux-accounts/pkg/types"
)

// PickSessionAdapterForTest exposes pickSessionAdapter for unit testing.
func (r *AccountPoolRouter) PickSessionAdapterForTest(adapters []types.ProviderAdapter, req *types.ChatRequest, nativeAvailable, strongerThanFree bool) types.ProviderAdapter {
	return r.pickSessionAdapter(adapters, req, nativeAvailable, strongerThanFree)
}

// SetCooldownForTest exposes setCooldownDuration for unit testing.
func (r *AccountPoolRouter) SetCooldownForTest(id string, d time.Duration) {
	r.setCooldownDuration(id, d)
}

// ClearCooldownsForTest clears all cooldowns for unit testing.
func (r *AccountPoolRouter) ClearCooldownsForTest() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cooldownMap = make(map[string]time.Time)
}
