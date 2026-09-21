package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"amux-accounts/pkg/types"
)

const (
	defaultAffinityTTL = 45 * time.Minute
	numSessionShards   = 64
)

type affinityEntry struct {
	accountID string
	expiresAt time.Time
}

type sessionShard struct {
	mu     sync.RWMutex
	pinned map[string]affinityEntry
}

// SessionAffinity pins conversations to a single account to avoid ping-ponging
// between multiple accounts during multi-turn developer workflows.
// Implements concurrent sharding across 64 shards to reduce lock contention
// and eliminate latency during concurrent terminal multiplexing tasks.
type SessionAffinity struct {
	ttl    time.Duration
	shards [numSessionShards]sessionShard
	closed atomic.Bool
	stopCh chan struct{}
}

// NewSessionAffinity creates a new high-concurrency sharded SessionAffinity manager.
func NewSessionAffinity(ttl time.Duration) *SessionAffinity {
	if ttl <= 0 {
		ttl = defaultAffinityTTL
	}
	sa := &SessionAffinity{
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	for i := 0; i < numSessionShards; i++ {
		sa.shards[i].pinned = make(map[string]affinityEntry)
	}

	// Start concurrent non-blocking background cleanup
	go sa.backgroundCleaner()
	return sa
}

func (sa *SessionAffinity) getShard(sessionKey string) *sessionShard {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionKey))
	idx := h.Sum64() % uint64(numSessionShards)
	return &sa.shards[idx]
}

// backgroundCleaner periodically evicts expired sessions across all shards in parallel.
func (sa *SessionAffinity) backgroundCleaner() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-sa.stopCh:
			return
		case <-ticker.C:
			sa.cleanupExpired()
		}
	}
}

// cleanupExpired performs non-blocking concurrent eviction across all shards.
func (sa *SessionAffinity) cleanupExpired() {
	now := time.Now()
	var wg sync.WaitGroup
	wg.Add(numSessionShards)
	for i := 0; i < numSessionShards; i++ {
		go func(shard *sessionShard) {
			defer wg.Done()
			shard.mu.Lock()
			for k, v := range shard.pinned {
				if now.After(v.expiresAt) {
					delete(shard.pinned, k)
				}
			}
			shard.mu.Unlock()
		}(&sa.shards[i])
	}
	wg.Wait()
}

// Close gracefully stops the background cleaner.
func (sa *SessionAffinity) Close() {
	if sa.closed.CompareAndSwap(false, true) {
		close(sa.stopCh)
	}
}

// ExtractSessionKey extracts a consistent session identifier from an HTTP request or ChatRequest.
// Looks at:
// 1. HTTP Headers: X-Session-Id, Session-Id, X-Conversation-Id
// 2. ChatRequest Metadata or Client Context
// 3. First system prompt / conversation hash if long-lived
func ExtractSessionKey(r *http.Request, req *types.ChatRequest) string {
	if r != nil {
		if s := strings.TrimSpace(r.Header.Get("X-Claude-Code-Session-Id")); s != "" {
			return s
		}
		if s := strings.TrimSpace(r.Header.Get("x-claude-code-session-id")); s != "" {
			return s
		}
		if s := strings.TrimSpace(r.Header.Get("X-Session-Id")); s != "" {
			return s
		}
		if s := strings.TrimSpace(r.Header.Get("Session-Id")); s != "" {
			return s
		}
		if s := strings.TrimSpace(r.Header.Get("X-Conversation-Id")); s != "" {
			return s
		}
		if s := strings.TrimSpace(r.Header.Get("X-Request-Id")); s != "" {
			// Some clients keep the same request prefix for a thread
			parts := strings.Split(s, "-")
			if len(parts) >= 2 {
				return parts[0]
			}
		}
	}

	if req != nil {
		if strings.TrimSpace(req.SessionID) != "" {
			return strings.TrimSpace(req.SessionID)
		}
		if req.Metadata != nil {
			if s, ok := req.Metadata["session_id"].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
			if s, ok := req.Metadata["conversation_id"].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		// Derive from initial conversation turn fingerprint if there are multiple messages
		if len(req.Messages) > 1 {
			// Hash first user/system message content + project to identify the root thread
			h := sha256.New()
			if proj := req.Project(); proj != "" {
				h.Write([]byte(proj))
				h.Write([]byte("::"))
			}
			h.Write([]byte(req.Messages[0].Role))
			h.Write([]byte(":"))
			h.Write([]byte(req.Messages[0].Content))
			sum := hex.EncodeToString(h.Sum(nil))
			return "thread-" + sum[:16]
		}
	}

	return ""
}

// GetPinned returns the pinned account ID for this session key if active and not expired.
// Executes with sub-microsecond latency via shard-isolated read lock.
func (sa *SessionAffinity) GetPinned(sessionKey string) (string, bool) {
	if sessionKey == "" {
		return "", false
	}
	shard := sa.getShard(sessionKey)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	entry, ok := shard.pinned[sessionKey]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.accountID, true
}

// Pin records or refreshes the binding between a session key and an account ID.
// Uses fine-grained shard locking for concurrent multiplexed execution.
func (sa *SessionAffinity) Pin(sessionKey, accountID string) {
	if sessionKey == "" || accountID == "" {
		return
	}
	shard := sa.getShard(sessionKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.pinned[sessionKey] = affinityEntry{
		accountID: accountID,
		expiresAt: time.Now().Add(sa.ttl),
	}
}

// CheckAndPin binds sessionKey to targetAccount and reports whether an account switch occurred.
// Returns (isSwitch, previousAccount).
func (sa *SessionAffinity) CheckAndPin(sessionKey, targetAccount string) (bool, string) {
	if sessionKey == "" || targetAccount == "" {
		return false, ""
	}
	shard := sa.getShard(sessionKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	prevEntry, exists := shard.pinned[sessionKey]
	isSwitch := false
	prevAccount := ""

	if exists && now.Before(prevEntry.expiresAt) && prevEntry.accountID != "" && prevEntry.accountID != targetAccount {
		isSwitch = true
		prevAccount = prevEntry.accountID
	}

	shard.pinned[sessionKey] = affinityEntry{
		accountID: targetAccount,
		expiresAt: now.Add(sa.ttl),
	}
	return isSwitch, prevAccount
}

// Unpin removes the session binding (e.g. after a rate-limit or failover).
func (sa *SessionAffinity) Unpin(sessionKey string) {
	if sessionKey == "" {
		return
	}
	shard := sa.getShard(sessionKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	delete(shard.pinned, sessionKey)
}

// UnpinAccount removes all sessions pinned to an account concurrently across all shards.
func (sa *SessionAffinity) UnpinAccount(accountID string) {
	if accountID == "" {
		return
	}
	var wg sync.WaitGroup
	wg.Add(numSessionShards)
	for i := 0; i < numSessionShards; i++ {
		go func(shard *sessionShard) {
			defer wg.Done()
			shard.mu.Lock()
			for k, v := range shard.pinned {
				if v.accountID == accountID {
					delete(shard.pinned, k)
				}
			}
			shard.mu.Unlock()
		}(&sa.shards[i])
	}
	wg.Wait()
}

// ActivePinsCount returns the number of active pinned sessions counted concurrently.
func (sa *SessionAffinity) ActivePinsCount() int {
	now := time.Now()
	var total int64
	var wg sync.WaitGroup
	wg.Add(numSessionShards)
	for i := 0; i < numSessionShards; i++ {
		go func(shard *sessionShard) {
			defer wg.Done()
			shard.mu.RLock()
			var cnt int64
			for _, v := range shard.pinned {
				if now.Before(v.expiresAt) {
					cnt++
				}
			}
			shard.mu.RUnlock()
			atomic.AddInt64(&total, cnt)
		}(&sa.shards[i])
	}
	wg.Wait()
	return int(total)
}
