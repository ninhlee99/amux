package guard

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ParseRetryAfter parses standard HTTP Retry-After header (seconds or RFC1123).
func ParseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	val := strings.TrimSpace(h.Get("Retry-After"))
	if val == "" {
		val = strings.TrimSpace(h.Get("retry-after"))
	}
	if val == "" {
		return 0
	}
	if sec, err := strconv.Atoi(val); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

var (
	// ErrAccountInBackoff is returned when a caller tries to send on an account
	// currently in a rate-limit cooldown backoff window.
	ErrAccountInBackoff = errors.New("guard: account is currently in cooldown backoff")
)

// PacerConfig controls rate-limit cooldown behavior.
type PacerConfig struct {
	// InitialBackoff is the starting backoff duration when a 429 is received without Retry-After.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration cap.
	MaxBackoff time.Duration
}

// DefaultPacerConfig provides safe production defaults.
func DefaultPacerConfig() PacerConfig {
	return PacerConfig{
		InitialBackoff: 30 * time.Second,
		MaxBackoff:     15 * time.Minute,
	}
}

type backoffState struct {
	until    time.Time
	attempts int
}

// Pacer tracks per-account rate-limit cooldowns. It performs no artificial
// throttling or delay — the only wait it ever imposes is honoring a
// provider's own 429 / Retry-After cooldown, so a rate-limited account isn't
// hammered again until the provider says it's safe to retry.
type Pacer struct {
	mu           sync.Mutex
	cfg          PacerConfig
	backoffState map[string]*backoffState
}

// NewPacer creates a new rate-limit cooldown tracker with the given configuration.
func NewPacer(cfg PacerConfig) *Pacer {
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 30 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 15 * time.Minute
	}
	return &Pacer{
		cfg:          cfg,
		backoffState: make(map[string]*backoffState),
	}
}

// InBackoff checks if an account is currently cooling down due to rate limits.
func (p *Pacer) InBackoff(accountID string) (bool, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, exists := p.backoffState[accountID]
	if !exists {
		return false, 0
	}
	now := time.Now()
	if now.Before(st.until) {
		return true, st.until.Sub(now)
	}
	// Expired
	delete(p.backoffState, accountID)
	return false, 0
}

// RecordRateLimit registers a rate-limit event for an account and computes exponential backoff.
func (p *Pacer) RecordRateLimit(accountID string, retryAfter time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.backoffState[accountID]
	if st == nil {
		st = &backoffState{}
		p.backoffState[accountID] = st
	}
	st.attempts++

	var wait time.Duration
	if retryAfter > 0 {
		// Respect upstream Retry-After + small safety buffer.
		wait = retryAfter + 2*time.Second
	} else {
		// Exponential backoff: base * 2^(attempts-1)
		mult := 1 << (st.attempts - 1)
		if mult > 32 {
			mult = 32
		}
		wait = time.Duration(mult) * p.cfg.InitialBackoff
	}

	if wait > p.cfg.MaxBackoff {
		wait = p.cfg.MaxBackoff
	}

	st.until = time.Now().Add(wait)
	return wait
}

// ClearBackoff clears any backoff for an account upon successful turn.
func (p *Pacer) ClearBackoff(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.backoffState, accountID)
}

// Pace checks whether the account is in a rate-limit cooldown. Unlike prior
// versions, it never sleeps or injects artificial spacing/jitter — it either
// returns immediately, or returns ErrAccountInBackoff if the provider itself
// rate-limited this account and the cooldown window hasn't elapsed yet.
func (p *Pacer) Pace(ctx context.Context, accountID string, isWeb bool) error {
	if in, _ := p.InBackoff(accountID); in {
		return ErrAccountInBackoff
	}
	return nil
}

// Reset clears pacing and backoff for a specific account.
func (p *Pacer) Reset(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.backoffState, accountID)
}

// ResetAll resets all accounts state in the pacer.
func (p *Pacer) ResetAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backoffState = make(map[string]*backoffState)
}
