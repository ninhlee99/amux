package ctxshrink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

const (
	// DefaultDeterministicTTL is default cache TTL for deterministic responses
	DefaultDeterministicTTL = 30 * time.Minute
	// MaxDeterministicEntries caps the cache to avoid unbounded RAM usage
	MaxDeterministicEntries = 500
)

// CachedReplay holds the complete recorded response for replaying to clients.
type CachedReplay struct {
	ContentType  string            `json:"content_type"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	SSEEvents    [][]byte          `json:"sse_events,omitempty"`
	InputTokens  int               `json:"input_tokens"`
	OutputTokens int               `json:"output_tokens"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

// Serve writes the cached response or streams SSE events to the response writer.
func (c *CachedReplay) Serve(w http.ResponseWriter, stream bool) error {
	if stream {
		if len(c.SSEEvents) == 0 {
			return fmt.Errorf("no sse events in cached replay")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		var flusher http.Flusher
		if f, ok := w.(http.Flusher); ok {
			flusher = f
		}
		for _, ev := range c.SSEEvents {
			if _, err := w.Write(ev); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		return nil
	}

	if len(c.Body) == 0 {
		return fmt.Errorf("empty body in cached replay")
	}
	if c.ContentType != "" {
		w.Header().Set("Content-Type", c.ContentType)
	}
	for k, v := range c.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(c.Body)
	return err
}

// RecordingWriter intercepts response writes and records SSE events and body bytes for caching.
type RecordingWriter struct {
	http.ResponseWriter
	flusher http.Flusher
	events  [][]byte
	buf     bytes.Buffer
	mu      sync.Mutex
}

// NewRecordingWriter wraps an http.ResponseWriter to capture streaming/buffered responses.
func NewRecordingWriter(w http.ResponseWriter) *RecordingWriter {
	var f http.Flusher
	if fl, ok := w.(http.Flusher); ok {
		f = fl
	}
	return &RecordingWriter{
		ResponseWriter: w,
		flusher:        f,
	}
}

func (rw *RecordingWriter) Write(b []byte) (int, error) {
	rw.mu.Lock()
	cp := make([]byte, len(b))
	copy(cp, b)
	rw.events = append(rw.events, cp)
	rw.buf.Write(b)
	rw.mu.Unlock()
	return rw.ResponseWriter.Write(b)
}

func (rw *RecordingWriter) Flush() {
	if rw.flusher != nil {
		rw.flusher.Flush()
	}
}

// Events returns a snapshot of recorded streaming events.
func (rw *RecordingWriter) Events() [][]byte {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	out := make([][]byte, len(rw.events))
	copy(out, rw.events)
	return out
}

// Body returns all recorded body bytes.
func (rw *RecordingWriter) Body() []byte {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.buf.Bytes()
}

// DeterministicReplayCache caches exact responses for identical prompts when temperature=0.
// Works universally across all accounts and providers: when an identical query
// is sent, amux replays the exact result with 0 upstream tokens, 0 cost, and <2ms latency.
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
func (c *DeterministicReplayCache) ComputeHash(req *types.ChatRequest) (string, bool) {
	if req == nil {
		return "", false
	}
	// Only cache deterministic requests where temperature was explicitly set <= 0.05
	if !req.ExplicitTemperature || req.Temperature > 0.05 {
		return "", false
	}
	if len(req.Messages) == 0 {
		return "", false
	}

	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("model:%s\n", strings.TrimSpace(strings.ToLower(req.Model)))))
	h.Write([]byte(fmt.Sprintf("stream:%v\n", req.Stream)))
	if proj := req.Project(); proj != "" {
		h.Write([]byte("project:" + proj + "\n"))
	}
	if req.SessionID != "" {
		h.Write([]byte("session:" + req.SessionID + "\n"))
	}

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

// ComputeRawHash hashes arbitrary raw body bytes if temperature is explicitly <= 0.05 in JSON.
func (c *DeterministicReplayCache) ComputeRawHash(body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	var probe struct {
		Temperature *float64 `json:"temperature"`
		Stream      bool     `json:"stream"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Temperature == nil || *probe.Temperature > 0.05 {
		return "", false
	}
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("stream:%v\n", probe.Stream)))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), true
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

	if len(c.entries) >= MaxDeterministicEntries {
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v.ExpiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= MaxDeterministicEntries {
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

func defaultReplayCachePath() string {
	return filepath.Join(types.BaseDir(), "cache", "replay_cache.json")
}

// SaveSnapshot saves valid replay entries atomically to disk.
func (c *DeterministicReplayCache) SaveSnapshot(path string) error {
	if path == "" {
		path = defaultReplayCachePath()
	}
	c.mu.RLock()
	now := time.Now()
	valid := make(map[string]*CachedReplay)
	for k, v := range c.entries {
		if now.Before(v.ExpiresAt) {
			valid[k] = v
		}
	}
	c.mu.RUnlock()

	if len(valid) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(valid)
	if err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpFile, path)
}

// LoadSnapshot loads valid cached replays from disk.
func (c *DeterministicReplayCache) LoadSnapshot(path string) error {
	if path == "" {
		path = defaultReplayCachePath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var loaded map[string]*CachedReplay
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range loaded {
		if now.Before(v.ExpiresAt) {
			c.entries[k] = v
		}
	}
	return nil
}
