package ctxshrink

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

const (
	// Default pointer thresholds
	DefaultDeduplicateMinRunes = 300 // Only deduplicate tool outputs >= 300 runes (~80 tokens)
	DefaultPointerTTL          = 2 * time.Hour
	// MaxDedupEntries caps the in-memory store to prevent unbounded growth
	// on long-running proxy instances. When exceeded, oldest entries are evicted.
	MaxDedupEntries = 1000
)

// ContentPointer holds metadata about an offloaded/deduplicated tool result payload.
type ContentPointer struct {
	Hash        string
	Snippet     string // first ~120 runes for LLM awareness
	LineCount   int
	ByteLength  int
	Original    string
	FirstSeenAt time.Time
}

// ProjectToolStore manages content-addressable storage for a single project's large tool outputs.
type ProjectToolStore struct {
	mu         sync.RWMutex
	store      map[string]ContentPointer // hash -> pointer
	minRunes   int
	ttl        time.Duration
	maxEntries int
}

func newProjectToolStore(minRunes int, ttl time.Duration, maxEntries int) *ProjectToolStore {
	return &ProjectToolStore{
		store:      make(map[string]ContentPointer),
		minRunes:   minRunes,
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (s *ProjectToolStore) evictOldestLocked() {
	var oldestHash string
	var oldestTime time.Time
	for hash, ptr := range s.store {
		if oldestHash == "" || ptr.FirstSeenAt.Before(oldestTime) {
			oldestHash = hash
			oldestTime = ptr.FirstSeenAt
		}
	}
	if oldestHash != "" {
		delete(s.store, oldestHash)
	}
}

func (s *ProjectToolStore) putPointerLocked(ptr ContentPointer) {
	if s.maxEntries > 0 && len(s.store) >= s.maxEntries {
		if _, exists := s.store[ptr.Hash]; !exists {
			s.evictOldestLocked()
		}
	}
	s.store[ptr.Hash] = ptr
}

func (s *ProjectToolStore) hasValidPointer(hash string) bool {
	ptr, exists := s.store[hash]
	if !exists {
		return false
	}
	if time.Since(ptr.FirstSeenAt) > s.ttl {
		delete(s.store, hash)
		return false
	}
	return true
}

// GlobalToolDeduplicator manages content-addressable storage for large tool outputs,
// strictly isolated per project root. It detects identical outputs (e.g. repeated file reads,
// bash command outputs, build logs) across multiple turns or across account switches within
// the same project, replacing subsequent occurrences with a compact pointer reference.
type GlobalToolDeduplicator struct {
	mu         sync.RWMutex
	projects   map[string]*ProjectToolStore // project -> store
	minRunes   int
	ttl        time.Duration
	maxEntries int
}

var (
	defaultDedupOnce sync.Once
	globalDedup      *GlobalToolDeduplicator
)

// GlobalDeduplicator returns the singleton instance of GlobalToolDeduplicator.
func GlobalDeduplicator() *GlobalToolDeduplicator {
	defaultDedupOnce.Do(func() {
		globalDedup = NewGlobalToolDeduplicator(DefaultDeduplicateMinRunes, DefaultPointerTTL)
	})
	return globalDedup
}

// NewGlobalToolDeduplicator creates a new deduplicator instance.
func NewGlobalToolDeduplicator(minRunes int, ttl time.Duration) *GlobalToolDeduplicator {
	if minRunes <= 0 {
		minRunes = DefaultDeduplicateMinRunes
	}
	if ttl <= 0 {
		ttl = DefaultPointerTTL
	}
	return &GlobalToolDeduplicator{
		projects:   make(map[string]*ProjectToolStore),
		minRunes:   minRunes,
		ttl:        ttl,
		maxEntries: MaxDedupEntries,
	}
}

func normalizeProjectKey(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return "global"
	}
	return strings.TrimRight(strings.ReplaceAll(project, "\\", "/"), "/")
}

func (d *GlobalToolDeduplicator) getStore(project string) *ProjectToolStore {
	key := normalizeProjectKey(project)
	d.mu.RLock()
	s, ok := d.projects[key]
	d.mu.RUnlock()
	if ok {
		return s
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok = d.projects[key]; ok {
		return s
	}
	s = newProjectToolStore(d.minRunes, d.ttl, d.maxEntries)
	d.projects[key] = s
	return s
}

// HashContent computes SHA256 hex digest of string content.
func HashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// DeduplicateMessages traverses the conversation messages from oldest to newest within
// the store of a single project.
func (s *ProjectToolStore) DeduplicateMessages(msgs []types.ChatMessage, keepRecentToolResults int) []types.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}
	if keepRecentToolResults <= 0 {
		keepRecentToolResults = 2 // Always keep the latest 2 tool outputs completely full
	}

	// Identify indices of all tool result messages
	toolIndices := make([]int, 0, len(msgs))
	for i, m := range msgs {
		if strings.EqualFold(m.Role, "tool") {
			toolIndices = append(toolIndices, i)
		}
	}

	// Any tool message among the last keepRecentToolResults stays 100% full
	protectedSet := make(map[int]bool)
	startProtected := len(toolIndices) - keepRecentToolResults
	if startProtected < 0 {
		startProtected = 0
	}
	for _, idx := range toolIndices[startProtected:] {
		protectedSet[idx] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]types.ChatMessage, len(msgs))
	copy(out, msgs)

	// Track hashes seen in this specific conversation pass
	sessionSeen := make(map[string]bool)

	for i := range out {
		if !strings.EqualFold(out[i].Role, "tool") {
			continue
		}

		content := out[i].Content
		r := []rune(content)
		if len(r) < s.minRunes {
			continue
		}

		hash := HashContent(content)

		// If protected (recent turn), store in cache so future turns can deduplicate against it,
		// but do NOT modify this message itself.
		if protectedSet[i] {
			if _, exists := s.store[hash]; !exists {
				snippet := string(r)
				if len(r) > 120 {
					snippet = string(r[:120]) + "..."
				}
				s.putPointerLocked(ContentPointer{
					Hash:        hash,
					Snippet:     snippet,
					LineCount:   strings.Count(content, "\n") + 1,
					ByteLength:  len(content),
					Original:    content,
					FirstSeenAt: time.Now(),
				})
			}
			sessionSeen[hash] = true
			continue
		}

		// Historical tool message: check if seen before
		if sessionSeen[hash] || s.hasValidPointer(hash) {
			ptr := s.store[hash]
			snippet := ptr.Snippet
			if snippet == "" {
				if len(r) > 120 {
					snippet = string(r[:120]) + "..."
				} else {
					snippet = string(r)
				}
			}
			lines := ptr.LineCount
			if lines == 0 {
				lines = strings.Count(content, "\n") + 1
			}
			bytesLen := ptr.ByteLength
			if bytesLen == 0 {
				bytesLen = len(content)
			}

			// Format compact pointer
			out[i].Content = fmt.Sprintf("[amux dedup-cache: %s | %d lines, %d bytes | preview: %s | identical content omitted]",
				hash[:12], lines, bytesLen, strings.ReplaceAll(snippet, "\n", " "))
		} else {
			// First time seeing this historical content: store pointer in cache
			snippet := string(r)
			if len(r) > 120 {
				snippet = string(r[:120]) + "..."
			}
			s.putPointerLocked(ContentPointer{
				Hash:        hash,
				Snippet:     snippet,
				LineCount:   strings.Count(content, "\n") + 1,
				ByteLength:  len(content),
				Original:    content,
				FirstSeenAt: time.Now(),
			})
			sessionSeen[hash] = true
		}
	}

	return out
}

// DeduplicateMessages traverses the conversation messages from oldest to newest within the scope
// of the given project.
// For historical tool results (older than the last keepRecentToolResults), if their content
// has already been seen earlier in the conversation or in that project's cache, it replaces
// the repetitive text with a compact pointer summary:
// "[amux dedup-cache: hash=xxx, lines=N, bytes=M, preview: ... (identical to prior output)]"
// This saves massive token amounts when agents repeatedly read or re-read files.
func (d *GlobalToolDeduplicator) DeduplicateMessages(project string, msgs []types.ChatMessage, keepRecentToolResults int) []types.ChatMessage {
	return d.getStore(project).DeduplicateMessages(msgs, keepRecentToolResults)
}

// Reset clears the deduplicator memory across all projects.
func (d *GlobalToolDeduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.projects = make(map[string]*ProjectToolStore)
}

// ResetProject clears the deduplicator memory for a specific project.
func (d *GlobalToolDeduplicator) ResetProject(project string) {
	key := normalizeProjectKey(project)
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.projects, key)
}

// Size returns the count of cached content pointers across all projects.
func (d *GlobalToolDeduplicator) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	total := 0
	for _, store := range d.projects {
		store.mu.RLock()
		total += len(store.store)
		store.mu.RUnlock()
	}
	return total
}

// ProjectSize returns the count of cached content pointers for a specific project.
func (d *GlobalToolDeduplicator) ProjectSize(project string) int {
	key := normalizeProjectKey(project)
	d.mu.RLock()
	store, ok := d.projects[key]
	d.mu.RUnlock()
	if !ok || store == nil {
		return 0
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.store)
}

func defaultDedupCachePath() string {
	return filepath.Join(types.BaseDir(), "cache", "tool_dedup.json")
}

// SaveSnapshot saves all projects' deduplicator states atomically to disk.
func (d *GlobalToolDeduplicator) SaveSnapshot(path string) error {
	if path == "" {
		path = defaultDedupCachePath()
	}
	d.mu.RLock()
	now := time.Now()
	allValid := make(map[string]map[string]ContentPointer)
	for prj, store := range d.projects {
		store.mu.RLock()
		prjValid := make(map[string]ContentPointer)
		for k, v := range store.store {
			if now.Sub(v.FirstSeenAt) <= store.ttl {
				prjValid[k] = v
			}
		}
		store.mu.RUnlock()
		if len(prjValid) > 0 {
			allValid[prj] = prjValid
		}
	}
	d.mu.RUnlock()

	if len(allValid) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(allValid)
	if err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpFile, path)
}

// LoadSnapshot restores the deduplicator states from disk.
func (d *GlobalToolDeduplicator) LoadSnapshot(path string) error {
	if path == "" {
		path = defaultDedupCachePath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var nested map[string]map[string]ContentPointer
	if err := json.Unmarshal(data, &nested); err == nil && len(nested) > 0 {
		now := time.Now()
		for prj, ptrMap := range nested {
			store := d.getStore(prj)
			store.mu.Lock()
			for k, v := range ptrMap {
				if now.Sub(v.FirstSeenAt) <= store.ttl {
					store.store[k] = v
				}
			}
			store.mu.Unlock()
		}
		return nil
	}

	var legacy map[string]ContentPointer
	if err := json.Unmarshal(data, &legacy); err == nil && len(legacy) > 0 {
		store := d.getStore("global")
		store.mu.Lock()
		now := time.Now()
		for k, v := range legacy {
			if now.Sub(v.FirstSeenAt) <= store.ttl {
				store.store[k] = v
			}
		}
		store.mu.Unlock()
	}
	return nil
}
