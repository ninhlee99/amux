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
	// MaxDeterministicEntries caps the cache per project to avoid unbounded RAM usage
	MaxDeterministicEntries = 300
)

// CachedReplay holds the complete recorded response for replaying to clients.
type CachedReplay struct {
	ContentType  string            `json:"content_type"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	SSEEvents    [][]byte          `json:"sse_events,omitempty"`
	InputTokens  int               `json:"input_tokens"`
	OutputTokens int               `json:"output_tokens"`
	Project      string            `json:"project,omitempty"`
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

// ProjectReplayStore holds the deterministic cache entries scoped to a single project.
type ProjectReplayStore struct {
	mu         sync.RWMutex
	projectKey string
	entries    map[string]*CachedReplay
	ttl        time.Duration
	maxEntries int
}

func newProjectReplayStore(projectKey string, ttl time.Duration, maxEntries int) *ProjectReplayStore {
	return &ProjectReplayStore{
		projectKey: projectKey,
		entries:    make(map[string]*CachedReplay),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (s *ProjectReplayStore) get(key string) (*CachedReplay, bool) {
	if key == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return entry, true
}

func (s *ProjectReplayStore) put(key string, replay *CachedReplay) {
	if key == "" || replay == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.entries) >= s.maxEntries {
		now := time.Now()
		for k, v := range s.entries {
			if now.After(v.ExpiresAt) {
				delete(s.entries, k)
			}
		}
		if len(s.entries) >= s.maxEntries {
			evictCount := 0
			for k := range s.entries {
				delete(s.entries, k)
				evictCount++
				if evictCount >= 20 {
					break
				}
			}
		}
	}

	now := time.Now()
	replay.CreatedAt = now
	if replay.ExpiresAt.IsZero() {
		replay.ExpiresAt = now.Add(s.ttl)
	}
	s.entries[key] = replay
}

func (s *ProjectReplayStore) len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func (s *ProjectReplayStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*CachedReplay)
}

// DeterministicReplayCache caches exact responses for identical prompts when temperature=0,
// strictly isolated per project root.
type DeterministicReplayCache struct {
	mu         sync.RWMutex
	projects   map[string]*ProjectReplayStore // projectKey -> store
	ttl        time.Duration
	maxEntries int
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
		projects:   make(map[string]*ProjectReplayStore),
		ttl:        ttl,
		maxEntries: MaxDeterministicEntries,
	}
}

func (c *DeterministicReplayCache) getStore(project string) *ProjectReplayStore {
	key := normalizeProjectKey(project)
	c.mu.RLock()
	s, ok := c.projects[key]
	c.mu.RUnlock()
	if ok {
		return s
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok = c.projects[key]; ok {
		return s
	}
	s = newProjectReplayStore(key, c.ttl, c.maxEntries)
	c.projects[key] = s
	return s
}

// ComputeHash computes a deterministic SHA256 digest of the request scoped to its project.
func (c *DeterministicReplayCache) ComputeHash(req *types.ChatRequest) (string, bool) {
	if req == nil {
		return "", false
	}
	return c.ComputeHashForProject(req.Project(), req)
}

// ComputeHashForProject computes a deterministic SHA256 digest scoped to the given project.
func (c *DeterministicReplayCache) ComputeHashForProject(project string, req *types.ChatRequest) (string, bool) {
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
	if p := normalizeProjectKey(project); p != "" {
		h.Write([]byte("project:" + p + "\n"))
	}
	h.Write([]byte(fmt.Sprintf("model:%s\n", strings.TrimSpace(strings.ToLower(req.Model)))))
	h.Write([]byte(fmt.Sprintf("stream:%v\n", req.Stream)))
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
	return c.ComputeRawHashForProject("", body)
}

// ComputeRawHashForProject hashes raw body bytes with project scoping.
func (c *DeterministicReplayCache) ComputeRawHashForProject(project string, body []byte) (string, bool) {
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
	if p := normalizeProjectKey(project); p != "" {
		h.Write([]byte("project:" + p + "\n"))
	}
	h.Write([]byte(fmt.Sprintf("stream:%v\n", probe.Stream)))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), true
}

// Get returns the cached replay if present and not expired (searches global and all projects).
func (c *DeterministicReplayCache) Get(key string) (*CachedReplay, bool) {
	return c.GetForProject("", key)
}

// GetForProject returns the cached replay for the specified project.
func (c *DeterministicReplayCache) GetForProject(project, key string) (*CachedReplay, bool) {
	if key == "" {
		return nil, false
	}
	store := c.getStore(project)
	if entry, found := store.get(key); found {
		return entry, true
	}
	// Fallback to global store if not found in project store and project was non-empty
	if normalizeProjectKey(project) != "global" {
		return c.getStore("global").get(key)
	}
	return nil, false
}

// Put stores a replay in the cache.
func (c *DeterministicReplayCache) Put(key string, replay *CachedReplay) {
	c.PutForProject("", key, replay)
}

// PutForProject stores a replay in the project-scoped store.
func (c *DeterministicReplayCache) PutForProject(project, key string, replay *CachedReplay) {
	if key == "" || replay == nil {
		return
	}
	if replay.Project == "" {
		replay.Project = project
	}
	store := c.getStore(project)
	store.put(key, replay)
}

// InvalidateProject clears all cached replays for a specific project.
func (c *DeterministicReplayCache) InvalidateProject(project string) {
	key := normalizeProjectKey(project)
	c.mu.Lock()
	if s, ok := c.projects[key]; ok {
		s.clear()
		delete(c.projects, key)
	}
	c.mu.Unlock()

	// Also remove project cache directory if exists
	pDir := types.ProjectCacheDir(project)
	_ = os.Remove(filepath.Join(pDir, "replay.json"))
}

// Size returns total active entries across all project stores.
func (c *DeterministicReplayCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := 0
	for _, s := range c.projects {
		total += s.len()
	}
	return total
}

// Clear clears all project stores.
func (c *DeterministicReplayCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.projects {
		s.clear()
	}
	c.projects = make(map[string]*ProjectReplayStore)
}

func defaultReplayCachePath() string {
	return filepath.Join(types.BaseDir(), "cache", "replay_cache.json")
}

// SaveSnapshot saves valid replay entries atomically to disk, writing both per-project
// cache files (~/.am/projects/<slug>/replay.json) and the unified cache file.
func (c *DeterministicReplayCache) SaveSnapshot(path string) error {
	if path == "" {
		path = defaultReplayCachePath()
	}
	c.mu.RLock()
	now := time.Now()
	allValid := make(map[string]map[string]*CachedReplay)
	flatValid := make(map[string]*CachedReplay)

	for prjKey, store := range c.projects {
		store.mu.RLock()
		prjValid := make(map[string]*CachedReplay)
		for k, v := range store.entries {
			if now.Before(v.ExpiresAt) {
				prjValid[k] = v
				flatValid[k] = v
			}
		}
		store.mu.RUnlock()
		if len(prjValid) > 0 {
			allValid[prjKey] = prjValid
		}
	}
	c.mu.RUnlock()

	// 1. Save per-project cache files
	for prjKey, entries := range allValid {
		if prjKey != "global" && len(entries) > 0 {
			pDir := types.ProjectCacheDir(prjKey)
			if err := os.MkdirAll(pDir, 0o700); err == nil {
				if b, err := json.Marshal(entries); err == nil {
					tmp := fmt.Sprintf("%s.tmp.%d", filepath.Join(pDir, "replay.json"), time.Now().UnixNano())
					if err := os.WriteFile(tmp, b, 0o600); err == nil {
						_ = os.Rename(tmp, filepath.Join(pDir, "replay.json"))
					}
				}
			}
		}
	}

	// 2. Save unified cache file
	if len(flatValid) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(flatValid)
	if err != nil {
		return err
	}
	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpFile, path)
}

// LoadSnapshot loads valid cached replays from disk, reading from both per-project
// cache directories and the unified snapshot.
func (c *DeterministicReplayCache) LoadSnapshot(path string) error {
	if path == "" {
		path = defaultReplayCachePath()
	}

	// 1. Load from unified file
	data, err := os.ReadFile(path)
	if err == nil {
		var loaded map[string]*CachedReplay
		if err := json.Unmarshal(data, &loaded); err == nil {
			now := time.Now()
			for k, v := range loaded {
				if now.Before(v.ExpiresAt) {
					prj := v.Project
					if prj == "" {
						prj = "global"
					}
					c.getStore(prj).put(k, v)
				}
			}
		}
	}

	// 2. Scan per-project directories (~/.am/projects/*/replay.json)
	projectsDir := filepath.Join(types.BaseDir(), "projects")
	entries, err := os.ReadDir(projectsDir)
	if err == nil {
		now := time.Now()
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			rPath := filepath.Join(projectsDir, e.Name(), "replay.json")
			rData, err := os.ReadFile(rPath)
			if err != nil {
				continue
			}
			var pLoaded map[string]*CachedReplay
			if err := json.Unmarshal(rData, &pLoaded); err == nil {
				store := c.getStore(e.Name())
				for k, v := range pLoaded {
					if now.Before(v.ExpiresAt) {
						store.put(k, v)
					}
				}
			}
		}
	}

	return nil
}
