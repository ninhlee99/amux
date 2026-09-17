package router

import "time"

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
