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
	AutoRotate   *bool                  `json:"auto_rotate,omitempty"` // true by default; if false, excluded from auto-rotation/switch
	ThresholdPct *float64               `json:"threshold_pct,omitempty"` // Per-account threshold override (0.0 to 100.0); nil uses default
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Config represents the system-wide flat identity configuration.
type Config struct {
	ThresholdPct float64    `json:"threshold_pct"` // Default 95.0
	Identities   []Identity `json:"identities"`
}

// CanAutoRotate reports whether this identity is eligible for automatic rotation/failover.
// Defaults to true unless explicitly set to false in AutoRotate or Metadata["manual_only"].
func (id Identity) CanAutoRotate() bool {
	if id.AutoRotate != nil {
		return *id.AutoRotate
	}
	if id.Metadata != nil {
		if v, ok := id.Metadata["auto_rotate"].(bool); ok {
			return v
		}
		if v, ok := id.Metadata["manual_only"].(bool); ok {
			return !v
		}
	}
	return true
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

// Email returns the associated account email or "-" if it is an API key or missing.
func (id Identity) Email() string {
	if id.Tier == TierAPIKey || id.AuthType == string(AuthAPIKey) {
		return "-"
	}
	if id.Metadata != nil {
		if em, ok := id.Metadata["email"].(string); ok && strings.TrimSpace(em) != "" {
			return strings.TrimSpace(em)
		}
	}
	if em, ok := id.Credentials["account"]; ok && strings.TrimSpace(em) != "" {
		return strings.TrimSpace(em)
	}
	if em, ok := id.Credentials["email"]; ok && strings.TrimSpace(em) != "" {
		return strings.TrimSpace(em)
	}
	return "-"
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

// GetThreshold returns the effective failover threshold percentage for this identity.
func (id Identity) GetThreshold(identities []Identity, baseThreshold float64) float64 {
	return GetAccountThreshold(id, identities, baseThreshold)
}

