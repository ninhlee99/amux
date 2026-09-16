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

// GlobalToolDeduplicator manages content-addressable storage for large tool outputs.
// It detects identical outputs (e.g. repeated file reads, bash command outputs, build logs)
// across multiple turns or across account switches, replacing subsequent occurrences with
// a compact pointer reference.
// Bounded by maxEntries: when full, the oldest (by FirstSeenAt) entry is evicted.
type GlobalToolDeduplicator struct {
	mu         sync.RWMutex
	store      map[string]ContentPointer // hash -> pointer
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
	d := &GlobalToolDeduplicator{
		store:      make(map[string]ContentPointer),
		minRunes:   minRunes,
		ttl:        ttl,
		maxEntries: MaxDedupEntries,
	}
	return d
}

// evictOldestLocked removes the single oldest entry from store.
// Must be called with d.mu held (write lock).
func (d *GlobalToolDeduplicator) evictOldestLocked() {
	var oldestHash string
	var oldestTime time.Time
	for hash, ptr := range d.store {
		if oldestHash == "" || ptr.FirstSeenAt.Before(oldestTime) {
			oldestHash = hash
			oldestTime = ptr.FirstSeenAt
		}
	}
	if oldestHash != "" {
		delete(d.store, oldestHash)
	}
}

// putPointerLocked stores a ContentPointer, evicting the oldest entry if maxEntries is reached.
// Must be called with d.mu held (write lock).
func (d *GlobalToolDeduplicator) putPointerLocked(ptr ContentPointer) {
	if d.maxEntries > 0 && len(d.store) >= d.maxEntries {
		if _, exists := d.store[ptr.Hash]; !exists {
			d.evictOldestLocked()
		}
	}
	d.store[ptr.Hash] = ptr
}

// HashContent computes SHA256 hex digest of string content.
func HashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// DeduplicateMessages traverses the conversation messages from oldest to newest.
// For historical tool results (older than the last keepRecentToolResults), if their content
// has already been seen earlier in the conversation or in the global cache, it replaces
// the repetitive text with a compact pointer summary:
// "[amux content-dedup: hash=xxx, lines=N, bytes=M, preview: ... (identical to prior output)]"
// This saves huge token amounts when agents repeatedly read or re-read files.
func (d *GlobalToolDeduplicator) DeduplicateMessages(msgs []types.ChatMessage, keepRecentToolResults int) []types.ChatMessage {
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

	d.mu.Lock()
	defer d.mu.Unlock()

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
		if len(r) < d.minRunes {
			continue
		}

		hash := HashContent(content)

		// If protected (recent turn), store in cache so future turns can deduplicate against it,
		// but do NOT modify this message itself.
		if protectedSet[i] {
			if _, exists := d.store[hash]; !exists {
				snippet := string(r)
				if len(r) > 120 {
					snippet = string(r[:120]) + "..."
				}
				d.putPointerLocked(ContentPointer{
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
		if sessionSeen[hash] || d.hasValidPointer(hash) {
			ptr := d.store[hash]
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
			d.putPointerLocked(ContentPointer{
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

func (d *GlobalToolDeduplicator) hasValidPointer(hash string) bool {
	ptr, exists := d.store[hash]
	if !exists {
		return false
	}
	if time.Since(ptr.FirstSeenAt) > d.ttl {
		delete(d.store, hash)
		return false
	}
	return true
}

// Reset clears the deduplicator memory (useful for testing).
func (d *GlobalToolDeduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.store = make(map[string]ContentPointer)
}

// Size returns the count of cached content pointers.
func (d *GlobalToolDeduplicator) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.store)
}

func defaultDedupCachePath() string {
	return filepath.Join(types.BaseDir(), "cache", "tool_dedup.json")
}

// SaveSnapshot saves the deduplicator state atomically to disk.
func (d *GlobalToolDeduplicator) SaveSnapshot(path string) error {
	if path == "" {
		path = defaultDedupCachePath()
	}
	d.mu.RLock()
	now := time.Now()
	valid := make(map[string]ContentPointer)
	for k, v := range d.store {
		if now.Sub(v.FirstSeenAt) <= d.ttl {
			valid[k] = v
		}
	}
	d.mu.RUnlock()

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

// LoadSnapshot restores the deduplicator state from disk.
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

	var loaded map[string]ContentPointer
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for k, v := range loaded {
		if now.Sub(v.FirstSeenAt) <= d.ttl {
			d.store[k] = v
		}
	}
	return nil
}
