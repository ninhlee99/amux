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
	Model        string                 `json:"model,omitempty"`
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
// Defaults to true unless explicitly set to false in AutoRotate, Metadata["manual_only"],
// or hard-disabled via Metadata["disabled"].
func (id Identity) CanAutoRotate() bool {
	if id.Metadata != nil {
		if v, ok := id.Metadata["disabled"].(bool); ok && v {
			return false
		}
		if v, ok := id.Metadata["manual_only"].(bool); ok && v {
			return false
		}
		if v, ok := id.Metadata["auto_rotate"].(bool); ok {
			return v
		}
	}
	if id.AutoRotate != nil {
		return *id.AutoRotate
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
		if em, ok := id.Metadata["account"].(string); ok && strings.TrimSpace(em) != "" {
			return strings.TrimSpace(em)
		}
		if em, ok := id.Metadata["profile_name"].(string); ok && strings.Contains(em, "@") {
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

// ModelName returns the configured model name or sensible default for the identity.
func (id Identity) ModelName() string {
	if strings.TrimSpace(id.Model) != "" {
		return strings.TrimSpace(id.Model)
	}
	if id.Metadata != nil {
		if m, ok := id.Metadata["model"].(string); ok && strings.TrimSpace(m) != "" {
			return strings.TrimSpace(m)
		}
	}
	p := strings.ToLower(id.ID)
	switch {
	case strings.HasPrefix(p, "claude") || strings.HasPrefix(p, "anthropic"):
		return "claude-3-7-sonnet"
	case strings.HasPrefix(p, "chatgpt"):
		return "gpt-4o"
	case strings.HasPrefix(p, "codex"):
		return "gpt-5.6-terra"
	case strings.HasPrefix(p, "gemini") || strings.HasPrefix(p, "antigravity") || strings.HasPrefix(p, "agy"):
		return "gemini-2.5-flash"
	case strings.HasPrefix(p, "openai"):
		return "gpt-4o"
	case strings.HasPrefix(p, "openrouter"):
		return "openrouter/auto"
	default:
		return "-"
	}
}

// GetThreshold returns the effective failover threshold percentage for this identity.
func (id Identity) GetThreshold(identities []Identity, baseThreshold float64) float64 {
	return GetAccountThreshold(id, identities, baseThreshold)
}

