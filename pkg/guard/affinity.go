package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

const (
	defaultAffinityTTL = 45 * time.Minute
)

type affinityEntry struct {
	accountID string
	expiresAt time.Time
}

// SessionAffinity pins conversations to a single account to avoid ping-ponging
// between multiple accounts during multi-turn developer workflows.
type SessionAffinity struct {
	mu      sync.RWMutex
	ttl     time.Duration
	pinned  map[string]affinityEntry // sessionKey -> affinityEntry
	cleaner *time.Ticker
}

// NewSessionAffinity creates a new SessionAffinity manager.
func NewSessionAffinity(ttl time.Duration) *SessionAffinity {
	if ttl <= 0 {
		ttl = defaultAffinityTTL
	}
	sa := &SessionAffinity{
		ttl:    ttl,
		pinned: make(map[string]affinityEntry),
	}
	return sa
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
			// Hash first user/system message content to identify the root thread
			h := sha256.New()
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
func (sa *SessionAffinity) GetPinned(sessionKey string) (string, bool) {
	if sessionKey == "" {
		return "", false
	}
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	entry, ok := sa.pinned[sessionKey]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.accountID, true
}

// Pin records or refreshes the binding between a session key and an account ID.
func (sa *SessionAffinity) Pin(sessionKey, accountID string) {
	if sessionKey == "" || accountID == "" {
		return
	}
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.pinned[sessionKey] = affinityEntry{
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
	sa.mu.Lock()
	defer sa.mu.Unlock()

	now := time.Now()
	prevEntry, exists := sa.pinned[sessionKey]
	isSwitch := false
	prevAccount := ""

	if exists && now.Before(prevEntry.expiresAt) && prevEntry.accountID != "" && prevEntry.accountID != targetAccount {
		isSwitch = true
		prevAccount = prevEntry.accountID
	}

	sa.pinned[sessionKey] = affinityEntry{
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
	sa.mu.Lock()
	defer sa.mu.Unlock()
	delete(sa.pinned, sessionKey)
}

// UnpinAccount removes all sessions pinned to an account that went offline or into quarantine.
func (sa *SessionAffinity) UnpinAccount(accountID string) {
	if accountID == "" {
		return
	}
	sa.mu.Lock()
	defer sa.mu.Unlock()
	for k, v := range sa.pinned {
		if v.accountID == accountID {
			delete(sa.pinned, k)
		}
	}
}

// ActivePinsCount returns the number of active pinned sessions.
func (sa *SessionAffinity) ActivePinsCount() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	now := time.Now()
	cnt := 0
	for _, v := range sa.pinned {
		if now.Before(v.expiresAt) {
			cnt++
		}
	}
	return cnt
}
