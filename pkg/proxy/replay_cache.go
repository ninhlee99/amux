package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

const (
	// Default cache TTL for deterministic responses
	DefaultDeterministicTTL = 30 * time.Minute
	// Maximum entries in deterministic cache to avoid memory leak
	MaxDeterministicEntries = 500
)

// CachedReplay holds the complete recorded response for replaying to clients.
type CachedReplay struct {
	ContentType  string            `json:"content_type"`
	Headers      map[string]string `json:"headers"`
	Body         []byte            `json:"body"`
	SSEEvents    [][]byte          `json:"sse_events,omitempty"`
	InputTokens  int               `json:"input_tokens"`
	OutputTokens int               `json:"output_tokens"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

// DeterministicReplayCache caches exact responses for identical prompts when temperature=0.
// This works universally across all accounts and providers: when an identical query
// (e.g. repeated lint check, repeated model prompt, or retry) is sent, amux replays
// the exact result with 0 upstream tokens, 0 cost, and <5ms response time.
type DeterministicReplayCache struct {
	mu      sync.RWMutex
	entries map[string]*CachedReplay // SHA256 request hash -> CachedReplay
	ttl     time.Duration
}

var (
	replayCacheOnce sync.Once
	globalReplay    *DeterministicReplayCache
)

// GlobalReplayCache returns the singleton DeterministicReplayCache instance.
func GlobalReplayCache() *DeterministicReplayCache {
	replayCacheOnce.Do(func() {
		globalReplay = NewDeterministicReplayCache(DefaultDeterministicTTL)
	})
	return globalReplay
}

// NewDeterministicReplayCache creates a new replay cache.
func NewDeterministicReplayCache(ttl time.Duration) *DeterministicReplayCache {
	if ttl <= 0 {
		ttl = DefaultDeterministicTTL
	}
	return &DeterministicReplayCache{
		entries: make(map[string]*CachedReplay),
		ttl:     ttl,
	}
}

// ComputeHash computes a deterministic SHA256 digest of the normalized request.
// Only caches requests where Temperature == 0 (deterministic).
func (c *DeterministicReplayCache) ComputeHash(req *types.ChatRequest) (string, bool) {
	if req == nil {
		return "", false
	}
	// Only cache deterministic requests (temperature == 0)
	if req.Temperature > 0.05 {
		return "", false
	}
	if len(req.Messages) == 0 {
		return "", false
	}

	h := sha256.New()
	// Model
	h.Write([]byte("model:"))
	h.Write([]byte(strings.TrimSpace(strings.ToLower(req.Model))))
	h.Write([]byte("\n"))

	// Messages
	for _, m := range req.Messages {
		h.Write([]byte("role:"))
		h.Write([]byte(strings.ToLower(strings.TrimSpace(m.Role))))
		h.Write([]byte("\n"))
		h.Write([]byte("content:"))
		h.Write([]byte(m.Content))
		h.Write([]byte("\n"))
		if m.ToolCallID != "" {
			h.Write([]byte("tool_call_id:"))
			h.Write([]byte(m.ToolCallID))
			h.Write([]byte("\n"))
		}
		for _, tc := range m.ToolCalls {
			h.Write([]byte("tc_name:"))
			h.Write([]byte(tc.Name))
			h.Write([]byte("tc_args:"))
			h.Write([]byte(tc.Arguments))
			h.Write([]byte("\n"))
		}
	}

	// Tools
	for _, t := range req.Tools {
		h.Write([]byte("tool:"))
		h.Write([]byte(t.Name))
		h.Write([]byte("\n"))
		h.Write([]byte(t.Description))
		h.Write([]byte("\n"))
		h.Write(t.InputSchema)
		h.Write([]byte("\n"))
	}

	return hex.EncodeToString(h.Sum(nil)), true
}

// ComputeRawHash hashes arbitrary raw body bytes if temperature is 0 in JSON.
func (c *DeterministicReplayCache) ComputeRawHash(body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	var probe struct {
		Temperature *float64 `json:"temperature"`
	}
	if err := json.Unmarshal(body, &probe); err == nil {
		if probe.Temperature != nil && *probe.Temperature > 0.05 {
			return "", false
		}
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]), true
}

// Get returns the cached replay if present and not expired.
func (c *DeterministicReplayCache) Get(key string) (*CachedReplay, bool) {
	if key == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return entry, true
}

// Put stores a replay in the cache.
func (c *DeterministicReplayCache) Put(key string, replay *CachedReplay) {
	if key == "" || replay == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if exceeding limit
	if len(c.entries) >= MaxDeterministicEntries {
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v.ExpiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= MaxDeterministicEntries {
			// Randomly evict 10%
			evictCount := 0
			for k := range c.entries {
				delete(c.entries, k)
				evictCount++
				if evictCount >= 50 {
					break
				}
			}
		}
	}

	now := time.Now()
	replay.CreatedAt = now
	if replay.ExpiresAt.IsZero() {
		replay.ExpiresAt = now.Add(c.ttl)
	}
	c.entries[key] = replay
}

// Size returns the active cache count.
func (c *DeterministicReplayCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear clears the cache.
func (c *DeterministicReplayCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CachedReplay)
}
