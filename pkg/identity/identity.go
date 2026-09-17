package identity

import (
	"strings"
	"time"
)

// IdentityTier defines the cost and prioritization tier of an account.
type IdentityTier string

const (
	TierSubscription IdentityTier = "subscription"
	TierWeb          IdentityTier = "web"
	TierAPIKey       IdentityTier = "api_key"
)

// IdentityAuthType defines how the identity authenticates.
type IdentityAuthType string

const (
	AuthOAuth         IdentityAuthType = "oauth"
	AuthCDP           IdentityAuthType = "cdp"
	AuthAPIKey        IdentityAuthType = "api_key"
	AuthSessionCookie IdentityAuthType = "session_cookie"
)

// DefaultThresholdPct is the default usage threshold (95%) for multi-account pools.
const DefaultThresholdPct = 95.0

// Identity represents a flat, canonical developer identity across providers and tiers.
// Group hierarchies are completely eliminated.
type Identity struct {
	ID           string                 `json:"id"`
	Provider     string                 `json:"provider"`      // "anthropic", "openai", "gemini", "cursor"
	Tier         IdentityTier           `json:"tier"`          // subscription, web, api_key
	AuthType     string                 `json:"auth_type"`     // oauth, cdp, api_key, session_cookie
	Credentials  map[string]string      `json:"credentials"`   // access_token, refresh_token, api_key, cookies, etc.
	UsagePercent float64                `json:"usage_percent"` // 0.0 to 100.0
	ResetAt      int64                  `json:"reset_at,omitempty"`
	Active       bool                   `json:"active"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Config represents the system-wide flat identity configuration.
type Config struct {
	ThresholdPct float64    `json:"threshold_pct"` // Default 95.0
	Identities   []Identity `json:"identities"`
}

// IsSubscription reports whether this identity is billed as a fixed-cost subscription.
func (id Identity) IsSubscription() bool {
	return id.Tier == TierSubscription || strings.EqualFold(string(id.Tier), "subscription")
}

// CanonicalProvider normalizes provider strings into anthropic, openai, gemini, cursor.
func CanonicalProvider(p string) string {
	s := strings.ToLower(strings.TrimSpace(p))
	switch {
	case strings.Contains(s, "claude") || strings.Contains(s, "anthropic"):
		return "anthropic"
	case strings.Contains(s, "codex") || strings.Contains(s, "openai") || strings.Contains(s, "chatgpt"):
		return "openai"
	case strings.Contains(s, "gemini") || strings.Contains(s, "agy") || strings.Contains(s, "antigravity"):
		return "gemini"
	case strings.Contains(s, "cursor"):
		return "cursor"
	default:
		return s
	}
}

// FormatResetTime returns human-readable duration until reset.
func (id Identity) FormatResetTime() string {
	if id.ResetAt <= 0 {
		return "unknown"
	}
	t := time.Unix(id.ResetAt, 0)
	rem := time.Until(t)
	if rem <= 0 {
		return "now"
	}
	rem = rem.Round(time.Minute)
	return strings.TrimSpace(strings.ReplaceAll(rem.String(), "0s", ""))
}
