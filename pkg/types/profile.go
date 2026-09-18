package types

import (
	"strings"
	"time"
)

// Artifact is one file or keychain entry that belongs to a tool's login state.
type Artifact struct {
	// Kind is "file" or "keychain".
	Kind string `json:"kind"`
	// Path is the absolute path for Kind=="file" (may contain ~).
	Path string `json:"path,omitempty"`
	// Service / Account identify a macOS generic-password item for Kind=="keychain".
	Service string `json:"service,omitempty"`
	Account string `json:"account,omitempty"`
	// Optional means a missing artifact is not an error when saving.
	Optional bool `json:"optional,omitempty"`
	// AccountField is a dotted path into a JSON file whose value names the
	// logged-in account (used by am current). Only meaningful for files.
	AccountField string `json:"accountField,omitempty"`
}

// ToolSpec describes how to snapshot / restore one CLI's auth state.
type ToolSpec struct {
	Name      string     `json:"name"`
	Artifacts []Artifact `json:"artifacts"`
}

// ProfileMeta represents metadata for a stored profile bundle on disk.
type ProfileMeta struct {
	Name    string    `json:"name"`
	Tool    string    `json:"tool"`
	Account string    `json:"account"`
	Saved   time.Time `json:"saved"`
	// Disabled means auto-rotate and `am sw` skip this profile until `am on`.
	Disabled bool `json:"disabled,omitempty"`
	// Proxy specifies an egress proxy URL (http://, https://, socks5://) for this profile.
	Proxy string `json:"proxy,omitempty"`
	// Plan indicates subscription tier ("pro", "team", "free").
	Plan string `json:"plan,omitempty"`
	ID string `json:"-"`
}

// ToAccount converts a ProfileMeta to a flat Account struct.
func (p ProfileMeta) ToAccount() Account {
	id := p.Name
	if id == "" {
		id = p.ID
	}
	isSub := IsSubscriptionTier(p.Plan)
	if p.Plan == "" && (p.Tool == "claude" || p.Tool == "antigravity") {
		isSub = true
	}
	typ := AccountTypeSubscription
	if !isSub {
		typ = AccountTypeWeb
	}
	email := p.Account
	if strings.TrimSpace(email) == "" {
		email = "-"
	}
	return Account{
		ID:           id,
		Email:        email,
		Provider:     p.Tool,
		Type:         typ,
		AuthType:     "oauth",
		UsagePercent: 0,
		Active:       !p.Disabled,
	}
}

// ProfileEntry is an artifact payload within a profile bundle.
type ProfileEntry struct {
	Artifact Artifact `json:"artifact"`
	// Data is the file bytes, or for keychain the raw secret string bytes.
	Data []byte `json:"data"`
}

// Window is one Anthropic rate-limit window (5h or 7d), straight from the
// anthropic-ratelimit-unified-{5h,7d}-{utilization,reset} headers. This is
// the same data Claude Code's own `/usage` reads, so `am status` mirrors it
// exactly instead of re-deriving a fraction from remaining/limit counts
// (which some window variants don't send).
type Window struct {
	Used    float64 // utilization, 0..1
	ResetAt time.Time
	Known   bool
}

// Token holds in-memory OAuth credentials and rate-limit states.
type Token struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
	Account   string
	Remaining float64 // last seen unified-remaining fraction (-1 unknown)
	ResetAt   time.Time

	// Separate 5h/7d windows. -1/zero-value = unknown until a response
	// carries the corresponding header.
	FiveH  Window
	SevenD Window
}
