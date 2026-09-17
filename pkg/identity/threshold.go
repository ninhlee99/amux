package identity


// GetEffectiveThreshold returns the quota threshold percentage for a provider.
// Rule:
// - Multi-Account Pool: Failover triggers when active subscription account reaches threshold_pct (default: 95.0%).
// - Single-Account Pool: If a provider pool contains ONLY 1 subscription account, allow it to reach 100.0% capacity before triggering fallback.
func GetEffectiveThreshold(provider string, identities []Identity, baseThreshold float64) float64 {
	if baseThreshold <= 0 {
		baseThreshold = DefaultThresholdPct
	}
	canon := CanonicalProvider(provider)
	subCount := 0
	for _, id := range identities {
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() {
			subCount++
		}
	}
	if subCount <= 1 {
		return 100.0
	}
	return baseThreshold
}

// ShouldFailover reports whether an identity has reached or exceeded its effective threshold.
func ShouldFailover(id Identity, identities []Identity, baseThreshold float64) bool {
	if !id.IsSubscription() {
		return id.UsagePercent >= 100.0
	}
	threshold := GetEffectiveThreshold(id.Provider, identities, baseThreshold)
	return id.UsagePercent >= threshold
}

// AllSubscriptionsExhausted reports whether ALL subscription accounts for a provider have hit their effective threshold.
func AllSubscriptionsExhausted(provider string, identities []Identity, baseThreshold float64) bool {
	canon := CanonicalProvider(provider)
	subCount := 0
	exhaustedCount := 0
	for _, id := range identities {
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() {
			subCount++
			if ShouldFailover(id, identities, baseThreshold) {
				exhaustedCount++
			}
		}
	}
	if subCount == 0 {
		return false
	}
	return subCount == exhaustedCount
}

// HasAvailableSubscription finds an active subscription account under threshold for the provider.
func HasAvailableSubscription(provider string, identities []Identity, baseThreshold float64) (*Identity, bool) {
	canon := CanonicalProvider(provider)
	for i := range identities {
		id := &identities[i]
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() && !ShouldFailover(*id, identities, baseThreshold) {
			return id, true
		}
	}
	return nil, false
}

// NextSubscription finds the next available subscription account in rotation.
func NextSubscription(provider string, currentID string, identities []Identity, baseThreshold float64) (*Identity, bool) {
	canon := CanonicalProvider(provider)
	var subs []Identity
	currentIdx := -1
	for _, id := range identities {
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() {
			if id.ID == currentID {
				currentIdx = len(subs)
			}
			subs = append(subs, id)
		}
	}
	if len(subs) == 0 {
		return nil, false
	}

	// Try accounts after current in circular order
	for step := 1; step <= len(subs); step++ {
		idx := (currentIdx + step) % len(subs)
		candidate := subs[idx]
		if candidate.ID != currentID && !ShouldFailover(candidate, identities, baseThreshold) {
			return &candidate, true
		}
	}

	return nil, false
}
