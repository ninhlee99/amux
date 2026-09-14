package guard

import (
	"context"
	"net/http"
	"time"

	"amux-accounts/pkg/types"
)

var (
	globalHealth   = NewHealthTracker()
	globalPacer    = NewPacer(DefaultPacerConfig())
	globalAffinity = NewSessionAffinity(45 * time.Minute)
)

// GlobalHealth returns the singleton health tracker instance.
func GlobalHealth() *HealthTracker {
	return globalHealth
}

// GlobalPacer returns the singleton traffic pacer instance.
func GlobalPacer() *Pacer {
	return globalPacer
}

// GlobalAffinity returns the singleton session affinity instance.
func GlobalAffinity() *SessionAffinity {
	return globalAffinity
}

// Sanitize scrubs sensitive internal headers before dispatching outbound requests.
func Sanitize(req *http.Request) {
	SanitizeOutboundRequest(req)
}

// Pace coordinates request cadence and enforces backoff delay.
func Pace(ctx context.Context, accountID string, isWeb bool) error {
	return globalPacer.Pace(ctx, accountID, isWeb)
}

// RecordSuccess marks a successful turn for the account in both health and pacer.
func RecordSuccess(accountID string) {
	globalHealth.RecordSuccess(accountID)
	globalPacer.ClearBackoff(accountID)
}

// RecordError updates health score, applies backoff, and may quarantine the account.
func RecordError(accountID string, err error) {
	if err == nil {
		return
	}
	globalHealth.RecordError(accountID, err)
	if classifyError(err) == classRateLimit {
		globalPacer.RecordRateLimit(accountID, 0)
	}
}

// ResetAll resets all tracking state across health, pacer, and affinity.
func ResetAll() {
	globalHealth.ResetAll()
	globalPacer.ResetAll()
}

// IsQuarantined checks whether an account is quarantined from active rotation.
func IsQuarantined(accountID string) (bool, time.Duration, string) {
	return globalHealth.IsQuarantined(accountID)
}

// GetAffinityAccount resolves any sticky account bound to this request or context.
func GetAffinityAccount(r *http.Request, req *types.ChatRequest) (string, bool) {
	key := ExtractSessionKey(r, req)
	if key == "" {
		return "", false
	}
	return globalAffinity.GetPinned(key)
}

// PinSession binds a session to an account for subsequent conversation turns.
func PinSession(r *http.Request, req *types.ChatRequest, accountID string) {
	key := ExtractSessionKey(r, req)
	if key != "" && accountID != "" {
		globalAffinity.Pin(key, accountID)
	}
}

// Status returns a serializable snapshot of the anti-ban protection layer status.
func Status() map[string]any {
	reports := globalHealth.GetAllReports()
	return map[string]any{
		"active_pinned_sessions": globalAffinity.ActivePinsCount(),
		"accounts":               reports,
	}
}
