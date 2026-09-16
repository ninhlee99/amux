package provider

import (
	"log"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultMaxTurnsPerConversation rotates the server-side thread once a project's
	// conversation accumulates this many turns, preventing web UIs from slowing down
	// or hitting token limits.
	DefaultMaxTurnsPerConversation = 25

	// DefaultConversationTTL expires conversations inactive for longer than this period.
	DefaultConversationTTL = 2 * time.Hour

	// MaxTrackedProjects caps the number of projects tracked in RAM.
	MaxTrackedProjects = 50
)

// ProjectConversation holds the state of an active web chat conversation for a specific project.
type ProjectConversation struct {
	ID          string            // conversation UUID or ID on the upstream web service
	ParentID    string            // parent message ID (used by ChatGPT web)
	Metadata    []string          // metadata tokens (used by Gemini web)
	ProjectRoot string            // project directory or git root
	SessionID   string            // last session ID that interacted with this thread
	TurnCount   int               // number of message turns sent in this thread
	CreatedAt   time.Time         // when the conversation was initiated
	LastUsedAt  time.Time         // last request timestamp
}

// ProjectConversationManager tracks and rotates server-side conversation threads per project.
// Ensures complete cross-project isolation while maximizing turn reuse within the same project.
type ProjectConversationManager struct {
	mu           sync.Mutex
	adapterID    string
	convs        map[string]*ProjectConversation // projectRoot -> conversation
	maxTurns     int
	ttl          time.Duration
}

// NewProjectConversationManager creates a conversation manager for an adapter.
func NewProjectConversationManager(adapterID string, maxTurns int, ttl time.Duration) *ProjectConversationManager {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurnsPerConversation
	}
	if ttl <= 0 {
		ttl = DefaultConversationTTL
	}
	return &ProjectConversationManager{
		adapterID: adapterID,
		convs:     make(map[string]*ProjectConversation),
		maxTurns:  maxTurns,
		ttl:       ttl,
	}
}

// NormalizeProjectKey cleanses and canonicalizes project roots for map indexing.
func NormalizeProjectKey(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return "global"
	}
	return filepathClean(project)
}

func filepathClean(p string) string {
	return strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
}

// GetActive retrieves the active conversation for a project if still valid and within turn limits.
// Returns nil if not found, expired, or turn limit reached.
func (m *ProjectConversationManager) GetActive(project string) (*ProjectConversation, bool) {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.convs[key]
	if !ok || c == nil || c.ID == "" {
		return nil, false
	}
	// Check turn limit
	if m.maxTurns > 0 && c.TurnCount >= m.maxTurns {
		log.Printf("%s: project %s reached conversation turn limit (%d/%d) — rotating",
			m.adapterID, key, c.TurnCount, m.maxTurns)
		delete(m.convs, key)
		return nil, false
	}
	// Check TTL
	if m.ttl > 0 && time.Since(c.LastUsedAt) > m.ttl {
		log.Printf("%s: project %s conversation expired (idle %v) — rotating",
			m.adapterID, key, time.Since(c.LastUsedAt).Round(time.Minute))
		delete(m.convs, key)
		return nil, false
	}

	c.LastUsedAt = time.Now()
	// Return a copy to avoid data races
	cp := *c
	return &cp, true
}

// Register stores or updates an active conversation for a project.
func (m *ProjectConversationManager) Register(project, sessionID, convID, parentID string, meta []string) {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.convs) >= MaxTrackedProjects {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range m.convs {
			if oldestKey == "" || v.LastUsedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.LastUsedAt
			}
		}
		if oldestKey != "" {
			delete(m.convs, oldestKey)
		}
	}

	c, exists := m.convs[key]
	if !exists || c == nil || c.ID != convID {
		m.convs[key] = &ProjectConversation{
			ID:          convID,
			ParentID:    parentID,
			Metadata:    meta,
			ProjectRoot: key,
			SessionID:   sessionID,
			TurnCount:   1,
			CreatedAt:   time.Now(),
			LastUsedAt:  time.Now(),
		}
		log.Printf("%s: initialized conversation %s for project %s", m.adapterID, convID, key)
	} else {
		c.ParentID = parentID
		if len(meta) > 0 {
			c.Metadata = meta
		}
		if sessionID != "" {
			c.SessionID = sessionID
		}
		c.TurnCount++
		c.LastUsedAt = time.Now()
	}
}

// ResetProject removes the conversation thread for a specific project.
func (m *ProjectConversationManager) ResetProject(project string) {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.convs, key)
	log.Printf("%s: cleared conversation for project %s", m.adapterID, key)
}

// ResetAll clears all project conversation threads on this adapter.
func (m *ProjectConversationManager) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.convs = make(map[string]*ProjectConversation)
	log.Printf("%s: cleared all project conversations", m.adapterID)
}

// ProjectTurnCount returns the current turn count for a project's active conversation.
func (m *ProjectConversationManager) ProjectTurnCount(project string) int {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.convs[key]; ok && c != nil {
		return c.TurnCount
	}
	return 0
}
