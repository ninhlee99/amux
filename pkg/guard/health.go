package guard

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

// HealthStatus represents the categoric health of an account.
type HealthStatus string

const (
	StatusHealthy     HealthStatus = "healthy"     // Score 80-100
	StatusDegraded    HealthStatus = "degraded"    // Score 40-79
	StatusQuarantined HealthStatus = "quarantined" // Score < 40 or critical repeated auth/ban flags
)

const (
	defaultQuarantineDuration = 45 * time.Minute
	authQuarantineDuration    = 60 * time.Minute
)

// HealthReport summarizes the current health of an account.
type HealthReport struct {
	AccountID          string       `json:"accountId"`
	Score              int          `json:"score"`
	Status             HealthStatus `json:"status"`
	ConsecutiveErrors  int          `json:"consecutiveErrors"`
	ConsecutiveAuthErr int          `json:"consecutiveAuthErr"`
	LastSuccess        time.Time    `json:"lastSuccess,omitempty"`
	LastError          time.Time    `json:"lastError,omitempty"`
	LastErrorMessage   string       `json:"lastErrorMessage,omitempty"`
	QuarantinedUntil   time.Time    `json:"quarantinedUntil,omitempty"`
	QuarantineReason   string       `json:"quarantineReason,omitempty"`
}

type accountHealthEntry struct {
	score              int
	consecutiveErrors  int
	consecutiveAuthErr int
	consecutive429     int
	lastSuccess        time.Time
	lastError          time.Time
	lastErrorMessage   string
	quarantinedUntil   time.Time
	quarantineReason   string
}

// HealthTracker monitors account health metrics and enforces predictive quarantines.
type HealthTracker struct {
	mu       sync.RWMutex
	accounts map[string]*accountHealthEntry
}

// NewHealthTracker creates a new HealthTracker instance.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		accounts: make(map[string]*accountHealthEntry),
	}
}

func (h *HealthTracker) getOrCreateLocked(id string) *accountHealthEntry {
	e, exists := h.accounts[id]
	if !exists {
		e = &accountHealthEntry{
			score: 100,
		}
		h.accounts[id] = e
	}
	return e
}

// RecordSuccess rewards the account by recovering score and clearing consecutive error tallies.
func (h *HealthTracker) RecordSuccess(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e := h.getOrCreateLocked(id)
	e.consecutiveErrors = 0
	e.consecutiveAuthErr = 0
	e.consecutive429 = 0
	e.lastSuccess = time.Now()

	// Gradual recovery (+5 points per successful turn, up to 100)
	e.score += 5
	if e.score > 100 {
		e.score = 100
	}
}

// RecordRateLimit deducts points and quarantines if repeated.
func (h *HealthTracker) RecordRateLimit(id string, retryAfter time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e := h.getOrCreateLocked(id)
	e.consecutiveErrors++
	e.consecutive429++
	e.lastError = time.Now()
	e.lastErrorMessage = "429 rate limit reached"

	e.score -= 20
	if e.score < 0 {
		e.score = 0
	}

	// 3 consecutive 429s or score < 40 triggers quarantine
	if e.consecutive429 >= 3 || e.score < 40 {
		dur := defaultQuarantineDuration
		if retryAfter > dur {
			dur = retryAfter
		}
		e.quarantinedUntil = time.Now().Add(dur)
		e.quarantineReason = "repeated rate limit hits (cooling down to prevent ban)"
	}
}

// RecordAuthError deducts heavily. If 2 consecutive auth errors happen (e.g. 401/403 or session revoked),
// the account is immediately quarantined to stop automated ban sweeps.
func (h *HealthTracker) RecordAuthError(id string, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e := h.getOrCreateLocked(id)
	e.consecutiveErrors++
	e.consecutiveAuthErr++
	e.lastError = time.Now()
	e.lastErrorMessage = "auth failure: " + reason

	e.score -= 45
	if e.score < 0 {
		e.score = 0
	}

	if e.consecutiveAuthErr >= 2 || e.score < 40 {
		e.quarantinedUntil = time.Now().Add(authQuarantineDuration)
		e.quarantineReason = "consecutive auth failures (quarantined to protect credentials)"
	}
}

// RecordServerError registers 5xx or connection drops.
func (h *HealthTracker) RecordServerError(id string, code int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e := h.getOrCreateLocked(id)
	e.consecutiveErrors++
	e.lastError = time.Now()
	e.lastErrorMessage = msg

	e.score -= 10
	if e.score < 0 {
		e.score = 0
	}
	if e.score < 40 {
		e.quarantinedUntil = time.Now().Add(15 * time.Minute)
		e.quarantineReason = "repeated upstream server errors"
	}
}

// RecordError automatically categorizes an error and logs health impact.
func (h *HealthTracker) RecordError(id string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, types.ErrRateLimitReached) {
		h.RecordRateLimit(id, 0)
		return
	}
	if errors.Is(err, types.ErrAuthentication) {
		h.RecordAuthError(id, err.Error())
		return
	}

	errStr := strings.ToLower(err.Error())

	// Payload-specific limits (context length / token limit / model support) are client/request mismatches,
	// NOT account health degradations.
	if strings.Contains(errStr, "context_length") || strings.Contains(errStr, "context length") ||
		strings.Contains(errStr, "reduce the length") || strings.Contains(errStr, "exceeds this model's context") ||
		strings.Contains(errStr, "not supported when using codex") {
		return
	}

	// Cloudflare / Turnstile challenges are anti-bot / rate-limiting barriers, not invalid credentials
	if strings.Contains(errStr, "cloudflare") || strings.Contains(errStr, "challenge-platform") ||
		strings.Contains(errStr, "cf_chl") || strings.Contains(errStr, "turnstile") {
		h.RecordRateLimit(id, 5*time.Minute)
		return
	}

	if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
		h.RecordRateLimit(id, 0)
		return
	}
	if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") {
		h.RecordAuthError(id, err.Error())
		return
	}
	h.RecordServerError(id, http.StatusInternalServerError, err.Error())
}

// IsQuarantined reports whether an account is quarantined, remaining time, and the reason.
func (h *HealthTracker) IsQuarantined(id string) (bool, time.Duration, string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e, exists := h.accounts[id]
	if !exists {
		return false, 0, ""
	}

	now := time.Now()
	if !e.quarantinedUntil.IsZero() && now.Before(e.quarantinedUntil) {
		return true, e.quarantinedUntil.Sub(now), e.quarantineReason
	}

	if !e.quarantinedUntil.IsZero() && now.After(e.quarantinedUntil) {
		// Quarantine expired: reset quarantine state and restore baseline degraded score
		e.quarantinedUntil = time.Time{}
		e.quarantineReason = ""
		e.score = 60
		e.consecutiveAuthErr = 0
		e.consecutive429 = 0
	}

	return false, 0, ""
}

// GetReport returns a snapshot of an account's health metrics.
func (h *HealthTracker) GetReport(id string) HealthReport {
	h.mu.RLock()
	defer h.mu.RUnlock()

	e, exists := h.accounts[id]
	if !exists {
		return HealthReport{
			AccountID: id,
			Score:     100,
			Status:    StatusHealthy,
		}
	}

	now := time.Now()
	var status HealthStatus
	if !e.quarantinedUntil.IsZero() && now.Before(e.quarantinedUntil) {
		status = StatusQuarantined
	} else if e.score >= 80 {
		status = StatusHealthy
	} else if e.score >= 40 {
		status = StatusDegraded
	} else {
		status = StatusQuarantined
	}

	return HealthReport{
		AccountID:          id,
		Score:              e.score,
		Status:             status,
		ConsecutiveErrors:  e.consecutiveErrors,
		ConsecutiveAuthErr: e.consecutiveAuthErr,
		LastSuccess:        e.lastSuccess,
		LastError:          e.lastError,
		LastErrorMessage:   e.lastErrorMessage,
		QuarantinedUntil:   e.quarantinedUntil,
		QuarantineReason:   e.quarantineReason,
	}
}

// GetAllReports returns health summaries for all tracked accounts.
func (h *HealthTracker) GetAllReports() map[string]HealthReport {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make(map[string]HealthReport, len(h.accounts))
	for id := range h.accounts {
		out[id] = h.GetReport(id)
	}
	return out
}

// Reset clears health issues for an account (e.g. after manual credential refresh).
func (h *HealthTracker) Reset(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.accounts, id)
}

// ResetAll clears health tracking for all accounts.
func (h *HealthTracker) ResetAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accounts = make(map[string]*accountHealthEntry)
}
