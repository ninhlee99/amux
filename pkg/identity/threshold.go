package identity


// GetEffectiveThreshold returns the quota threshold percentage for a provider.
// Rule:
// - Multi-Account Pool: Failover triggers when active subscription account reaches threshold_pct (default: 95.0%).
// - Single-Account Pool: If a provider pool contains ONLY 1 subscription account eligible for auto-rotation,
//   allow it to reach 100.0% capacity before triggering fallback.
func GetEffectiveThreshold(provider string, identities []Identity, baseThreshold float64) float64 {
	if baseThreshold <= 0 {
		baseThreshold = DefaultThresholdPct
	}
	canon := CanonicalProvider(provider)
	subCount := 0
	for _, id := range identities {
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() && id.CanAutoRotate() {
			subCount++
		}
	}
	if subCount <= 1 {
		return 100.0
	}
	return baseThreshold
}

// GetAccountThreshold returns the failover threshold percentage for an identity.
// If the identity has an explicit ThresholdPct configured (> 0), that is returned.
// Otherwise, it falls back to the effective threshold for the provider.
func GetAccountThreshold(id Identity, identities []Identity, baseThreshold float64) float64 {
	if id.ThresholdPct != nil && *id.ThresholdPct > 0 {
		return *id.ThresholdPct
	}
	if id.Metadata != nil {
		if v, ok := id.Metadata["threshold_pct"].(float64); ok && v > 0 {
			return v
		}
		if v, ok := id.Metadata["threshold"].(float64); ok && v > 0 {
			return v
		}
	}
	if !id.IsSubscription() {
		return 100.0
	}
	return GetEffectiveThreshold(id.Provider, identities, baseThreshold)
}

// ShouldFailover reports whether an identity has reached or exceeded its effective threshold.
func ShouldFailover(id Identity, identities []Identity, baseThreshold float64) bool {
	threshold := GetAccountThreshold(id, identities, baseThreshold)
	return id.UsagePercent >= threshold
}

// AllSubscriptionsExhausted reports whether ALL subscription accounts for a provider (eligible for auto-rotation) have hit their effective threshold.
func AllSubscriptionsExhausted(provider string, identities []Identity, baseThreshold float64) bool {
	canon := CanonicalProvider(provider)
	subCount := 0
	exhaustedCount := 0
	for _, id := range identities {
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() && IsEnabled(id) && id.CanAutoRotate() {
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
// Accounts marked as manual-only (CanAutoRotate() == false) or disabled are excluded from automatic rotation.
func HasAvailableSubscription(provider string, identities []Identity, baseThreshold float64) (*Identity, bool) {
	canon := CanonicalProvider(provider)
	for i := range identities {
		id := &identities[i]
		if CanonicalProvider(id.Provider) == canon && id.IsSubscription() && IsEnabled(*id) && id.CanAutoRotate() && !ShouldFailover(*id, identities, baseThreshold) {
			return id, true
		}
	}
	return nil, false
}

// NextSubscription finds the next available subscription account in rotation.
// It skips accounts that are marked as manual-only (CanAutoRotate() == false).
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
		if candidate.ID != currentID && candidate.CanAutoRotate() && !ShouldFailover(candidate, identities, baseThreshold) {
			return &candidate, true
		}
	}

	return nil, false
}
