package provider

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"amux-accounts/pkg/types"
)

// extractJWTEmailAndTimestamp extracts email and issued-at (or pwd_auth_time/exp)
// timestamp from an unverified JWT token without external dependencies.
func extractJWTEmailAndTimestamp(tok string) (email string, timestamp int64) {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return "", 0
	}
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return "", 0
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", 0
	}

	// 1. Try to extract email
	if e, ok := claims["email"].(string); ok && strings.Contains(e, "@") {
		email = strings.TrimSpace(e)
	}
	if prof, ok := claims["https://api.openai.com/profile"].(map[string]any); ok && email == "" {
		if e, ok := prof["email"].(string); ok && strings.Contains(e, "@") {
			email = strings.TrimSpace(e)
		}
	}
	if authObj, ok := claims["https://api.openai.com/auth"].(map[string]any); ok && email == "" {
		if e, ok := authObj["email"].(string); ok && strings.Contains(e, "@") {
			email = strings.TrimSpace(e)
		}
	}
	if email == "" {
		if sub, ok := claims["sub"].(string); ok && strings.Contains(sub, "@") {
			email = strings.TrimSpace(sub)
		}
	}

	// 2. Extract timestamp (prefer iat > pwd_auth_time > exp)
	if v, ok := claims["iat"].(float64); ok && int64(v) > 0 {
		timestamp = int64(v)
	} else if v, ok := claims["pwd_auth_time"].(float64); ok && int64(v) > 0 {
		ts := int64(v)
		if ts > 1000000000000 { // milliseconds to seconds
			ts /= 1000
		}
		timestamp = ts
	} else if v, ok := claims["exp"].(float64); ok && int64(v) > 0 {
		timestamp = int64(v)
	}

	return email, timestamp
}

// providerLoginTimestamp extracts the latest login or token timestamp for a provider.
func providerLoginTimestamp(p ProviderConfig) int64 {
	var maxTS int64
	for _, tok := range []string{p.SessionToken, p.APIKey, p.RefreshToken} {
		if strings.TrimSpace(tok) != "" {
			_, ts := extractJWTEmailAndTimestamp(tok)
			if ts > maxTS {
				maxTS = ts
			}
		}
	}
	return maxTS
}

// hasAnyCredential reports if p contains any non-empty credential secrets.
func hasAnyCredential(p ProviderConfig) bool {
	return strings.TrimSpace(p.APIKey) != "" ||
		strings.TrimSpace(p.SessionToken) != "" ||
		strings.TrimSpace(p.SessionKey) != "" ||
		strings.TrimSpace(p.RefreshToken) != "" ||
		strings.TrimSpace(p.Cookies) != ""
}

// effectiveAccount returns the account email from Account field or decoded JWT.
func effectiveAccount(p ProviderConfig) string {
	if strings.TrimSpace(p.Account) != "" {
		return strings.TrimSpace(p.Account)
	}
	for _, tok := range []string{p.SessionToken, p.APIKey, p.RefreshToken} {
		if strings.TrimSpace(tok) != "" {
			if email, _ := extractJWTEmailAndTimestamp(tok); email != "" {
				return email
			}
		}
	}
	return ""
}

// isBetterProvider compares candidate against current best.
// Returns true if candidate is preferred over best (newest login wins).
func isBetterProvider(candidate, best ProviderConfig) bool {
	// Subscription tier ALWAYS beats Web tier for the same provider
	candSub := IsSubscriptionType(candidate.Type)
	bestSub := IsSubscriptionType(best.Type)
	if candSub != bestSub {
		return candSub
	}
	candPlanSub := types.IsSubscriptionTier(candidate.Plan)
	bestPlanSub := types.IsSubscriptionTier(best.Plan)
	if candPlanSub != bestPlanSub {
		return candPlanSub
	}

	candCred := hasAnyCredential(candidate)
	bestCred := hasAnyCredential(best)
	if candCred != bestCred {
		return candCred
	}

	candTS := providerLoginTimestamp(candidate)
	bestTS := providerLoginTimestamp(best)
	if candTS != bestTS {
		return candTS > bestTS // Latest login wins!
	}

	// Named identity ID preferred over legacy numeric ID
	candLegacy := isLegacyNumericID(candidate.ID)
	bestLegacy := isLegacyNumericID(best.ID)
	if candLegacy != bestLegacy {
		return !candLegacy
	}

	// Explicit account email preferred
	if (candidate.Account != "") != (best.Account != "") {
		return candidate.Account != ""
	}

	// Enabled preferred over disabled
	candEnabled := candidate.Enabled == nil || *candidate.Enabled
	bestEnabled := best.Enabled == nil || *best.Enabled
	if candEnabled != bestEnabled {
		return candEnabled
	}

	return false
}

// mergeProviderConfig copies non-empty settings from src to dest.
func mergeProviderConfig(dest *ProviderConfig, src ProviderConfig) {
	if dest.Account == "" && src.Account != "" {
		dest.Account = src.Account
	}
	if dest.Plan == "" && src.Plan != "" {
		dest.Plan = src.Plan
	}
	if dest.Model == "" && src.Model != "" {
		dest.Model = src.Model
	}
	if dest.BaseURL == "" && src.BaseURL != "" {
		dest.BaseURL = src.BaseURL
	}
	if dest.RefreshToken == "" && src.RefreshToken != "" {
		dest.RefreshToken = src.RefreshToken
	}
	if dest.SessionKey == "" && src.SessionKey != "" {
		dest.SessionKey = src.SessionKey
	}
	if dest.Cookies == "" && src.Cookies != "" {
		dest.Cookies = src.Cookies
	}
	if dest.SessionToken == "" && src.SessionToken != "" {
		dest.SessionToken = src.SessionToken
	}
	destSub := IsSubscriptionType(dest.Type)
	srcSub := IsSubscriptionType(src.Type)
	if !destSub && srcSub {
		dest.Type = src.Type
		dest.Plan = src.Plan
		dest.ID = src.ID
	}
	if isLegacyNumericID(dest.ID) && !isLegacyNumericID(src.ID) {
		dest.ID = src.ID
	}
}

// isGhostProvider reports whether a provider is a ghost entry with no usable
// credentials and no account email, or an invalid session token fragment.
func isGhostProvider(p ProviderConfig, all []ProviderConfig) bool {
	// Completely empty credentials and empty account
	if !hasAnyCredential(p) && strings.TrimSpace(p.Account) == "" {
		// If another provider of this type exists, this one is a ghost
		for _, other := range all {
			if other.ID != p.ID && other.Type == p.Type && (hasAnyCredential(other) || other.Account != "") {
				return true
			}
		}
	}
	// Malformed chatgpt cookie fragment stored as SessionToken with no email
	if p.Type == "chatgpt_web" && strings.HasPrefix(p.SessionToken, "__Secure") && strings.TrimSpace(p.Account) == "" {
		if email, _ := extractJWTEmailAndTimestamp(p.SessionToken); email == "" {
			return true
		}
	}
	return false
}

// areDuplicates checks whether p1 and p2 represent duplicate configurations
// for the same underlying account or credential.
func areDuplicates(p1, p2 ProviderConfig) bool {
	if p1.ID == p2.ID {
		return false
	}

	// 1. Same secret credential
	secrets1 := providerSecrets(p1)
	secrets2 := providerSecrets(p2)
	for _, s1 := range secrets1 {
		for _, s2 := range secrets2 {
			if sameSecret(s1, s2) {
				return true
			}
		}
	}

	// 2. Same canonical provider + same account identity (explicit or decoded from JWT)
	if CanonicalProvider(p1.Type) == CanonicalProvider(p2.Type) {
		acct1 := effectiveAccount(p1)
		acct2 := effectiveAccount(p2)
		if acct1 != "" && acct2 != "" && strings.EqualFold(acct1, acct2) {
			return true
		}
	}

	return false
}

// DeduplicateProviders analyzes all providers in path, eliminates duplicate
// accounts while keeping the one with the latest login timestamp, purges empty
// ghost accounts, and updates legacy numeric IDs.
func DeduplicateProviders(path string) (removed []string, err error) {
	f, err := LoadConfigFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if f == nil || len(f.Providers) == 0 {
		return nil, nil
	}

	// 1. Purge ghost providers
	var nonGhosts []ProviderConfig
	for _, p := range f.Providers {
		if isGhostProvider(p, f.Providers) {
			removed = append(removed, p.ID)
		} else {
			// Backfill Account if decoded from JWT and currently empty
			if p.Account == "" {
				if acct := effectiveAccount(p); acct != "" {
					p.Account = acct
				}
			}
			nonGhosts = append(nonGhosts, p)
		}
	}

	// 2. Group duplicate providers
	visited := make([]bool, len(nonGhosts))
	var kept []ProviderConfig

	for i := 0; i < len(nonGhosts); i++ {
		if visited[i] {
			continue
		}
		visited[i] = true
		cluster := []int{i}

		for j := i + 1; j < len(nonGhosts); j++ {
			if visited[j] {
				continue
			}
			// Check if j matches any member in cluster
			matches := false
			for _, cIdx := range cluster {
				if areDuplicates(nonGhosts[cIdx], nonGhosts[j]) {
					matches = true
					break
				}
			}
			if matches {
				cluster = append(cluster, j)
				visited[j] = true
			}
		}

		if len(cluster) == 1 {
			kept = append(kept, nonGhosts[i])
			continue
		}

		// Find best in cluster (latest login wins)
		bestIdx := cluster[0]
		for _, idx := range cluster[1:] {
			if isBetterProvider(nonGhosts[idx], nonGhosts[bestIdx]) {
				bestIdx = idx
			}
		}

		winner := nonGhosts[bestIdx]
		// Merge other cluster members into winner
		for _, idx := range cluster {
			if idx != bestIdx {
				mergeProviderConfig(&winner, nonGhosts[idx])
				removed = append(removed, nonGhosts[idx].ID)
			}
		}

		kept = append(kept, winner)
	}

	if len(removed) == 0 {
		return nil, nil
	}

	f.Providers = kept
	return removed, SaveConfigFile(path, f)
}
