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
	AutoRotate   *bool                  `json:"auto_rotate,omitempty"` // pool membership; nil = default (web/API in, subscription out)
	ThresholdPct *float64               `json:"threshold_pct,omitempty"` // Per-account threshold override (0.0 to 100.0); nil uses default
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Config represents the system-wide flat identity configuration.
type Config struct {
	Version      int        `json:"version,omitempty"`
	ThresholdPct float64    `json:"threshold_pct"` // Default 95.0
	Identities   []Identity `json:"identities"`
}

// CanAutoRotate reports whether this identity is in the rotation pool, i.e.
// amux may pick it (or switch to it) without being told to.
//
// Web and API-key accounts are in the pool unless taken out. Subscription
// accounts (the user's own Claude Code / Codex / Antigravity plans) are
// never in it by default: they join only when added by hand
// (`amux pool add <id>`). Hard-disabled accounts are never in it.
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
	return !id.IsSubscription()
}

// InPool is CanAutoRotate under the name the CLI uses.
func (id Identity) InPool() bool { return id.CanAutoRotate() }

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
		return "claude-opus-5-5"
	case strings.HasPrefix(p, "chatgpt"):
		return "gpt-6-luna"
	case strings.HasPrefix(p, "codex"):
		return "gpt-6.1-sol"
	case strings.HasPrefix(p, "gemini") || strings.HasPrefix(p, "antigravity") || strings.HasPrefix(p, "agy"):
		return "gemini-3.8-flash"
	case strings.HasPrefix(p, "openai"):
		return "gpt-6.1-sol"
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


// Pooled returns a pointer to true for Identity.AutoRotate (manual pool add).
func Pooled() *bool {
	v := true
	return &v
}

// DisplayProvider is the product name shown to users ("Codex", "ChatGPT Web",
// "Claude Code", …) instead of the internal ID.
func (id Identity) DisplayProvider() string {
	low := strings.ToLower(id.ID)
	switch {
	case strings.HasPrefix(low, "claude:web"), strings.HasPrefix(low, "claudeweb"):
		return "Claude Web"
	case strings.HasPrefix(low, "claude"):
		return "Claude Code"
	case strings.HasPrefix(low, "codex"):
		return "Codex"
	case strings.HasPrefix(low, "chatgpt"):
		return "ChatGPT Web"
	case strings.HasPrefix(low, "agy"), strings.HasPrefix(low, "antigravity"):
		return "Antigravity"
	case strings.HasPrefix(low, "gemini:web"), strings.HasPrefix(low, "geminiweb"):
		return "Gemini Web"
	case strings.HasPrefix(low, "gemini"):
		return "Gemini API"
	case strings.HasPrefix(low, "cursor"):
		return "Cursor"
	}
	name := strings.Split(id.ID, ":")[0]
	if name == "" {
		name = id.Provider
	}
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	if id.Tier == TierAPIKey {
		return name + " API"
	}
	return name
}

// Label names the account for messages: "Codex (me@x.com)", or just the
// provider when the email is unknown.
func (id Identity) Label() string {
	if em := id.Email(); em != "" && em != "-" {
		return id.DisplayProvider() + " (" + em + ")"
	}
	return id.DisplayProvider()
}

// ProductOfType maps an accounts.json provider type (or a profile tool name)
// to the product it logs in to — the same names DisplayProvider shows. One
// email may hold one account per product: ChatGPT Web and Codex are separate
// products even though both are OpenAI.
func ProductOfType(providerType string) string {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "chatgpt_web", "chatgpt":
		return "ChatGPT Web"
	case "codex_cli", "codex":
		return "Codex"
	case "claude_web":
		return "Claude Web"
	case "claude", "claude_code", "claude_cli", "claude_oauth":
		return "Claude Code"
	case "antigravity", "agy":
		return "Antigravity"
	case "gemini_web":
		return "Gemini Web"
	case "gemini":
		return "Gemini API"
	}
	return strings.ToLower(strings.TrimSpace(providerType))
}
