package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry holds gateway metrics in-memory without external dependencies.
type Registry struct {
	mu           sync.RWMutex
	requests     map[string]*atomic.Uint64
	tokens       map[string]*atomic.Uint64
	durations    map[string]*durationStats
	customGauges map[string]float64
}

type durationStats struct {
	count atomic.Uint64
	total atomic.Uint64 // sum in ms
}

var defaultRegistry = NewRegistry()

// Default returns the singleton metrics registry.
func Default() *Registry {
	return defaultRegistry
}

// NewRegistry creates a fresh metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		requests:     make(map[string]*atomic.Uint64),
		tokens:       make(map[string]*atomic.Uint64),
		durations:    make(map[string]*durationStats),
		customGauges: make(map[string]float64),
	}
}

// IncRequest increments the request counter for the given client, provider, and status ("ok" or "err").
func (r *Registry) IncRequest(client, provider, status string) {
	if client == "" {
		client = "unknown"
	}
	if provider == "" {
		provider = "unknown"
	}
	if status == "" {
		status = "ok"
	}
	key := fmt.Sprintf(`client="%s",provider="%s",status="%s"`, client, provider, status)

	r.mu.RLock()
	c, ok := r.requests[key]
	r.mu.RUnlock()
	if ok {
		c.Add(1)
		return
	}

	r.mu.Lock()
	if c, ok = r.requests[key]; !ok {
		c = &atomic.Uint64{}
		r.requests[key] = c
	}
	r.mu.Unlock()
	c.Add(1)
}

// ObserveDuration records request latency in milliseconds.
func (r *Registry) ObserveDuration(client, provider string, durationMs int64) {
	if durationMs < 0 {
		return
	}
	if client == "" {
		client = "unknown"
	}
	if provider == "" {
		provider = "unknown"
	}
	key := fmt.Sprintf(`client="%s",provider="%s"`, client, provider)

	r.mu.RLock()
	d, ok := r.durations[key]
	r.mu.RUnlock()
	if !ok {
		r.mu.Lock()
		if d, ok = r.durations[key]; !ok {
			d = &durationStats{}
			r.durations[key] = d
		}
		r.mu.Unlock()
	}
	d.count.Add(1)
	d.total.Add(uint64(durationMs))
}

// AddTokens adds in or out tokens for an account.
func (r *Registry) AddTokens(account, direction string, count int) {
	if count <= 0 {
		return
	}
	if account == "" {
		account = "unknown"
	}
	if direction != "in" && direction != "out" {
		direction = "out"
	}
	key := fmt.Sprintf(`account="%s",direction="%s"`, account, direction)

	r.mu.RLock()
	c, ok := r.tokens[key]
	r.mu.RUnlock()
	if ok {
		c.Add(uint64(count))
		return
	}

	r.mu.Lock()
	if c, ok = r.tokens[key]; !ok {
		c = &atomic.Uint64{}
		r.tokens[key] = c
	}
	r.mu.Unlock()
	c.Add(uint64(count))
}

// AccountHealthSummary is a minimal interface for passing health reports to metrics.
type AccountHealthSummary struct {
	Account  string
	Provider string
	Score    float64
}

// RenderPrometheus formats the metrics into Prometheus text exposition format (version 0.0.4).
func (r *Registry) RenderPrometheus(activeSessions int, healthReports []AccountHealthSummary) string {
	var sb strings.Builder

	// 1. amux_active_sessions
	sb.WriteString("# HELP amux_active_sessions Number of currently active IDE sessions connected to gateway.\n")
	sb.WriteString("# TYPE amux_active_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("amux_active_sessions %d\n\n", activeSessions))

	// 2. amux_requests_total
	sb.WriteString("# HELP amux_requests_total Total number of chat/completion requests handled by gateway.\n")
	sb.WriteString("# TYPE amux_requests_total counter\n")
	r.mu.RLock()
	var reqKeys []string
	for k := range r.requests {
		reqKeys = append(reqKeys, k)
	}
	sort.Strings(reqKeys)
	for _, k := range reqKeys {
		val := r.requests[k].Load()
		sb.WriteString(fmt.Sprintf("amux_requests_total{%s} %d\n", k, val))
	}
	sb.WriteString("\n")

	// 3. amux_request_duration_ms_avg
	sb.WriteString("# HELP amux_request_duration_ms_avg Average request duration in milliseconds.\n")
	sb.WriteString("# TYPE amux_request_duration_ms_avg gauge\n")
	var durKeys []string
	for k := range r.durations {
		durKeys = append(durKeys, k)
	}
	sort.Strings(durKeys)
	for _, k := range durKeys {
		d := r.durations[k]
		cnt := d.count.Load()
		tot := d.total.Load()
		var avg float64
		if cnt > 0 {
			avg = float64(tot) / float64(cnt)
		}
		sb.WriteString(fmt.Sprintf("amux_request_duration_ms_avg{%s} %.2f\n", k, avg))
	}
	sb.WriteString("\n")

	// 4. amux_tokens_total
	sb.WriteString("# HELP amux_tokens_total Total input and output tokens routed through gateway.\n")
	sb.WriteString("# TYPE amux_tokens_total counter\n")
	var tokKeys []string
	for k := range r.tokens {
		tokKeys = append(tokKeys, k)
	}
	sort.Strings(tokKeys)
	for _, k := range tokKeys {
		val := r.tokens[k].Load()
		sb.WriteString(fmt.Sprintf("amux_tokens_total{%s} %d\n", k, val))
	}
	r.mu.RUnlock()
	sb.WriteString("\n")

	// 5. amux_account_health
	if len(healthReports) > 0 {
		sb.WriteString("# HELP amux_account_health Health score of provider accounts (0-100).\n")
		sb.WriteString("# TYPE amux_account_health gauge\n")
		for _, h := range healthReports {
			sb.WriteString(fmt.Sprintf(`amux_account_health{account="%s",provider="%s"} %.1f`+"\n", h.Account, h.Provider, h.Score))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
