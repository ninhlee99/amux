package types

import "strings"

// AccountType classifies an account by billing/access model. All three types
// are first-class: capability parity does not depend on Type, only routing
// priority (subscription > web > api_key) and threshold behavior do.
type AccountType string

const (
	AccountTypeSubscription AccountType = "subscription"
	AccountTypeWeb          AccountType = "web"
	AccountTypeAPIKey       AccountType = "api_key"
)

// Account is a flat account record — no nested group hierarchy.
type Account struct {
	ID           string      `json:"id"`
	Email        string      `json:"email,omitempty"`
	Provider     string      `json:"provider"`      // "claude", "codex", "gemini", "cursor"
	Type         AccountType `json:"type"`           // subscription, web, api_key
	AuthType     string      `json:"auth_type"`      // oauth, cdp, api_key
	UsagePercent float64     `json:"usage_percent"`  // 0.0 to 100.0
	ResetAt      int64       `json:"reset_at,omitempty"`
	Active       bool        `json:"active"`
	ThresholdPct *float64    `json:"threshold_pct,omitempty"`
}

// IsSubscription reports whether this account is billed as a fixed-cost subscription.
func (a Account) IsSubscription() bool { return a.Type == AccountTypeSubscription }

// Config is the flat daemon configuration: a single threshold plus the account list.
type Config struct {
	ThresholdPct float64   `json:"threshold_pct"` // Default 95.0
	Accounts     []Account `json:"accounts"`
}

// DefaultThresholdPct is the failover trigger when a provider pool has more
// than one subscription account. A pool with exactly one subscription
// account ignores this and is allowed to reach 100% (see the single-account
// rule in the router/lifecycle package).
const DefaultThresholdPct = 95.0

// IsSubscriptionTier reports whether a given plan or tier string represents a paid subscription.
func IsSubscriptionTier(plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	return p == "pro" || p == "plus" || p == "team" || p == "enterprise" || p == "max" || p == "ultra" || p == "subscription"
}
