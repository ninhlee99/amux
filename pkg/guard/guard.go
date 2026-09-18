package guard

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

var (
	globalHealth   = NewHealthTracker()
	globalAffinity = NewSessionAffinity(45 * time.Minute)

	pacerMu        sync.Mutex
	accountLastReq = make(map[string]time.Time)
)

// GlobalHealth returns the singleton health tracker instance.
func GlobalHealth() *HealthTracker {
	return globalHealth
}

// GlobalAffinity returns the singleton session affinity instance.
func GlobalAffinity() *SessionAffinity {
	return globalAffinity
}

// Sanitize scrubs sensitive internal headers before dispatching outbound requests.
func Sanitize(req *http.Request) {
	SanitizeOutboundRequest(req)
}

// Pace coordinates request cadence: for web and API accounts, enforces a minimum
// spacing of 10-15 seconds per account between outgoing requests to prevent 429 rate limits.
// Subscription accounts (Claude / Codex CLI) bypass this pacing for zero-latency interactive execution.
func Pace(ctx context.Context, accountID string, isPaced bool) error {
	if !isPaced || accountID == "" {
		return nil
	}

	pacerMu.Lock()
	last := accountLastReq[accountID]
	var wait time.Duration
	if !last.IsZero() {
		// Random interval between 10s and 15s to add jitter and avoid robotic bursts
		jitterMs := int64(10000)
		if n, err := rand.Int(rand.Reader, big.NewInt(5001)); err == nil {
			jitterMs = 10000 + n.Int64()
		}
		interval := time.Duration(jitterMs) * time.Millisecond
		elapsed := time.Since(last)
		if elapsed < interval {
			wait = interval - elapsed
		}
	}
	// Reserve slot: next request will be spaced from target dispatch time
	accountLastReq[accountID] = time.Now().Add(wait)
	pacerMu.Unlock()

	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil
}

// RecordSuccess marks a successful turn for the account in health.
func RecordSuccess(accountID string) {
	globalHealth.RecordSuccess(accountID)
}

// RecordError updates health score and may quarantine the account.
func RecordError(accountID string, err error) {
	if err == nil {
		return
	}
	globalHealth.RecordError(accountID, err)
}

// ResetAll resets all tracking state across health, affinity, and request pacing.
func ResetAll() {
	globalHealth.ResetAll()
	pacerMu.Lock()
	accountLastReq = make(map[string]time.Time)
	pacerMu.Unlock()
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

// CheckSessionAccountSwitch checks if the session was previously on a different account
// and binds the session to targetAccount. Returns (isSwitch, prevAccount).
func CheckSessionAccountSwitch(r *http.Request, req *types.ChatRequest, targetAccount string) (bool, string) {
	key := ExtractSessionKey(r, req)
	if key == "" || targetAccount == "" {
		return false, ""
	}
	return globalAffinity.CheckAndPin(key, targetAccount)
}

// Status returns a serializable snapshot of the anti-ban protection layer status.
func Status() map[string]any {
	reports := globalHealth.GetAllReports()
	return map[string]any{
		"active_pinned_sessions": globalAffinity.ActivePinsCount(),
		"accounts":               reports,
	}
}
