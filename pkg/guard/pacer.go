package guard

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
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

// PacerConfig controls the pacing behavior.
type PacerConfig struct {
	// MinRequestInterval is the minimum duration between consecutive requests to the same account.
	MinRequestInterval time.Duration
	// WebJitterMin is minimum random jitter added to web-session requests.
	WebJitterMin time.Duration
	// WebJitterMax is maximum random jitter added to web-session requests.
	WebJitterMax time.Duration
	// InitialBackoff is the starting backoff duration when a 429 is received without Retry-After.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration cap.
	MaxBackoff time.Duration
}

// DefaultPacerConfig provides safe production defaults.
func DefaultPacerConfig() PacerConfig {
	return PacerConfig{
		MinRequestInterval: 250 * time.Millisecond,
		WebJitterMin:       100 * time.Millisecond,
		WebJitterMax:       350 * time.Millisecond,
		InitialBackoff:     30 * time.Second,
		MaxBackoff:         15 * time.Minute,
	}
}

type backoffState struct {
	until    time.Time
	attempts int
}

// Pacer coordinates request timing across accounts to eliminate bot-like burst patterns.
type Pacer struct {
	mu           sync.Mutex
	cfg          PacerConfig
	lastRequest  map[string]time.Time
	backoffState map[string]*backoffState
}

// NewPacer creates a new request pacer with the given configuration.
func NewPacer(cfg PacerConfig) *Pacer {
	if cfg.MinRequestInterval <= 0 {
		cfg.MinRequestInterval = 250 * time.Millisecond
	}
	if cfg.WebJitterMax <= cfg.WebJitterMin {
		cfg.WebJitterMin = 100 * time.Millisecond
		cfg.WebJitterMax = 350 * time.Millisecond
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 30 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 15 * time.Minute
	}
	return &Pacer{
		cfg:          cfg,
		lastRequest:  make(map[string]time.Time),
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
		// Respect upstream Retry-After + small safety buffer (1-3s)
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

	// Add random jitter of up to 10% to prevent thundering herd
	jitterMs, _ := rand.Int(rand.Reader, big.NewInt(1000))
	wait += time.Duration(jitterMs.Int64()) * time.Millisecond

	st.until = time.Now().Add(wait)
	return wait
}

// ClearBackoff clears any backoff for an account upon successful turn.
func (p *Pacer) ClearBackoff(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.backoffState, accountID)
}

// Pace waits the necessary duration to maintain healthy cadence before dispatching.
// For web sessions (isWeb == true), adds humanized random micro-jitter.
func (p *Pacer) Pace(ctx context.Context, accountID string, isWeb bool) error {
	p.mu.Lock()
	// 1. Check backoff first
	if st, ok := p.backoffState[accountID]; ok {
		now := time.Now()
		if now.Before(st.until) {
			p.mu.Unlock()
			return ErrAccountInBackoff
		}
		delete(p.backoffState, accountID)
	}

	// 2. Compute minimum spacing interval
	now := time.Now()
	var wait time.Duration
	if last, ok := p.lastRequest[accountID]; ok {
		elapsed := now.Sub(last)
		if elapsed < p.cfg.MinRequestInterval {
			wait = p.cfg.MinRequestInterval - elapsed
		}
	}

	// 3. Add humanized micro-jitter for web sessions
	if isWeb {
		jitterSpan := p.cfg.WebJitterMax - p.cfg.WebJitterMin
		if jitterSpan > 0 {
			r, _ := rand.Int(rand.Reader, big.NewInt(jitterSpan.Milliseconds()))
			wait += p.cfg.WebJitterMin + time.Duration(r.Int64())*time.Millisecond
		}
	}

	// Reserve the slot timestamp
	p.lastRequest[accountID] = now.Add(wait)
	p.mu.Unlock()

	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil
}

// Reset clears pacing and backoff for a specific account.
func (p *Pacer) Reset(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.lastRequest, accountID)
	delete(p.backoffState, accountID)
}

// ResetAll resets all accounts state in the pacer.
func (p *Pacer) ResetAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRequest = make(map[string]time.Time)
	p.backoffState = make(map[string]*backoffState)
}
