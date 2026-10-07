package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

const (
	// DefaultMaxTurnsPerConversation rotates the server-side thread once a project's
	// conversation accumulates this many turns, preventing web UIs from slowing down
	// or hitting token limits.
	DefaultMaxTurnsPerConversation = 25

	// DefaultMaxTokensPerConversation rotates the server-side thread once accumulated tokens
	// in this conversation exceed 25,000, preventing 413s or context penalties on web providers.
	DefaultMaxTokensPerConversation = 25000

	// DefaultConversationTTL expires conversations inactive for longer than this period.
	DefaultConversationTTL = 2 * time.Hour

	// MaxTrackedProjects caps the number of projects tracked in RAM.
	MaxTrackedProjects = 50
)

// ProjectConversation holds the state of an active web chat conversation for a specific project.
type ProjectConversation struct {
	ID          string    `json:"id"`                     // conversation UUID or ID on the upstream web service
	ParentID    string    `json:"parent_id,omitempty"`    // parent message ID (used by ChatGPT web)
	Metadata    []string  `json:"metadata,omitempty"`     // metadata tokens (used by Gemini web)
	ProjectRoot string    `json:"project_root"`           // project directory or git root
	SessionID   string    `json:"session_id,omitempty"`   // last session ID that interacted with this thread
	TurnCount   int       `json:"turn_count"`             // number of message turns sent in this thread
	TotalTokens int       `json:"total_tokens"`           // accumulated tokens consumed in this thread
	CreatedAt   time.Time `json:"created_at"`             // when the conversation was initiated
	LastUsedAt  time.Time `json:"last_used_at"`            // last request timestamp
	// Checkpoints record, per answered turn, the client history the thread
	// was sent and the upstream message that answered it. They let a later
	// request that branched off an earlier turn be recognised.
	Checkpoints []ThreadCheckpoint `json:"checkpoints,omitempty"`
}

// maxThreadCheckpoints bounds how far back a branched request can rejoin.
const maxThreadCheckpoints = 8

// HistoryMark fingerprints the client history a turn was answered from.
type HistoryMark struct {
	Len  int    `json:"len"`
	Hash string `json:"hash"`
}

// ThreadCheckpoint is the thread head (ParentID) after answering the
// client history described by Mark.
type ThreadCheckpoint struct {
	Mark     HistoryMark `json:"mark"`
	ParentID string      `json:"parent_id,omitempty"`
}

// HistoryMarkOf fingerprints msgs. The zero mark means "nothing to record".
func HistoryMarkOf(msgs []types.ChatMessage) HistoryMark {
	if len(msgs) == 0 {
		return HistoryMark{}
	}
	return HistoryMark{Len: len(msgs), Hash: historyHash(msgs)}
}

func historyHash(msgs []types.ChatMessage) string {
	h := sha256.New()
	for _, m := range msgs {
		h.Write([]byte(strings.ToLower(m.Role)))
		h.Write([]byte{0})
		h.Write([]byte(stripBillingHeader(m.Content)))
		h.Write([]byte{0})
		h.Write([]byte(m.ToolCallID))
		h.Write([]byte{0})
		for _, tc := range m.ToolCalls {
			h.Write([]byte(tc.ID))
			h.Write([]byte{0})
			h.Write([]byte(tc.Name))
			h.Write([]byte{0})
			h.Write([]byte(tc.Arguments))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ForkPoint reports whether msgs continue an earlier turn of c rather than
// its latest one, and if so the thread head right after that turn.
//
// Claude Code sends side requests (the "user stepped away" recap) on the
// main session with the main system prompt. They land on the live thread
// and leave a reply there that the client's own transcript never holds; the
// next real request then sits on the wrong branch, and ChatGPT answers that
// it has no repo access. The request's last assistant turn is the reply the
// thread gave to the checkpoint whose history is exactly what precedes it.
func (c *ProjectConversation) ForkPoint(msgs []types.ChatMessage) (parentID string, forked bool) {
	if c == nil || len(c.Checkpoints) < 2 {
		return "", false
	}
	last := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(msgs[i].Role, "assistant") {
			last = i
			break
		}
	}
	if last <= 0 {
		return "", false
	}
	var hash string
	for i := len(c.Checkpoints) - 1; i >= 0; i-- {
		cp := c.Checkpoints[i]
		if cp.Mark.Len != last {
			continue
		}
		if hash == "" {
			hash = historyHash(msgs[:last])
		}
		if cp.Mark.Hash != hash {
			continue
		}
		if i == len(c.Checkpoints)-1 {
			return "", false
		}
		return cp.ParentID, true
	}
	return "", false
}

// ProjectConversationManager tracks and rotates server-side conversation threads per project.
// Ensures complete cross-project isolation while maximizing turn reuse within the same project.
type ProjectConversationManager struct {
	mu        sync.Mutex
	adapterID string
	convs     map[string]*ProjectConversation // projectRoot -> conversation
	maxTurns  int
	maxTokens int
	ttl       time.Duration
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
		maxTokens: DefaultMaxTokensPerConversation,
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

// threadSep joins a project key and its per-session thread hash.
const threadSep = "#"

// ThreadKey scopes a web thread to one project, client session and system
// prompt. A live thread holds the system prompt from its first turn and later
// turns send only the delta, so a new Claude Code session, a side request
// with its own system prompt (title generation) or a changed system prompt
// must open its own thread instead of continuing another one.
func ThreadKey(req *types.ChatRequest) string {
	project := NormalizeProjectKey(req.Project())
	if req == nil {
		return project
	}
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(req.SessionID)))
	h.Write([]byte{0})
	// Only the leading system turns are the session's system prompt; Claude
	// Code also sends per-turn system notes (<total_tokens>, environment)
	// mid-conversation, which must not split the thread.
	for _, m := range req.Messages {
		if !strings.EqualFold(m.Role, "system") {
			break
		}
		h.Write([]byte(stripBillingHeader(m.Content)))
		h.Write([]byte{0})
	}
	return project + threadSep + hex.EncodeToString(h.Sum(nil))[:16]
}

// stripBillingHeader drops Claude Code's billing header lines, which can
// change on every request without changing the instructions.
func stripBillingHeader(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "x-anthropic-billing-header:") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// splitThreadKey returns the project part of a thread key and its thread
// hash ("" for a plain project key).
func splitThreadKey(key string) (project, thread string) {
	if i := strings.LastIndex(key, threadSep); i >= 0 {
		return key[:i], key[i+len(threadSep):]
	}
	return key, ""
}

func filepathClean(p string) string {
	return strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
}

func (m *ProjectConversationManager) snapshotPath(projectKey string) string {
	project, thread := splitThreadKey(projectKey)
	name := "conv_" + m.snapshotID()
	if thread != "" {
		name += "_" + thread
	}
	return filepath.Join(types.ProjectCacheDir(project), name+".json")
}

func (m *ProjectConversationManager) snapshotID() string {
	sanitizedID := strings.ReplaceAll(m.adapterID, ":", "_")
	return strings.ReplaceAll(sanitizedID, "/", "_")
}

func (m *ProjectConversationManager) saveProjectSnapshotLocked(projectKey string) {
	conv, ok := m.convs[projectKey]
	if !ok || conv == nil {
		p := m.snapshotPath(projectKey)
		_ = os.Remove(p)
		return
	}
	p := m.snapshotPath(projectKey)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(conv)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}

func (m *ProjectConversationManager) loadProjectSnapshotLocked(projectKey string) (*ProjectConversation, bool) {
	p := m.snapshotPath(projectKey)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var conv ProjectConversation
	if err := json.Unmarshal(data, &conv); err != nil {
		_ = os.Remove(p)
		return nil, false
	}
	// Check limits
	if m.ttl > 0 && time.Since(conv.LastUsedAt) > m.ttl {
		_ = os.Remove(p)
		return nil, false
	}
	if m.maxTurns > 0 && conv.TurnCount >= m.maxTurns {
		_ = os.Remove(p)
		return nil, false
	}
	if m.maxTokens > 0 && conv.TotalTokens >= m.maxTokens {
		_ = os.Remove(p)
		return nil, false
	}
	return &conv, true
}

// GetActive retrieves the active conversation for a project if still valid and within turn/token limits.
// Returns nil if not found, expired, or limits reached.
func (m *ProjectConversationManager) GetActive(project string) (*ProjectConversation, bool) {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.convs[key]
	if !ok || c == nil || c.ID == "" {
		// Try loading persisted snapshot from disk
		loaded, found := m.loadProjectSnapshotLocked(key)
		if !found || loaded == nil || loaded.ID == "" {
			return nil, false
		}
		c = loaded
		m.convs[key] = c
	}

	// Check turn limit
	if m.maxTurns > 0 && c.TurnCount >= m.maxTurns {
		log.Printf("%s: project %s reached conversation turn limit (%d/%d) — rotating",
			m.adapterID, key, c.TurnCount, m.maxTurns)
		delete(m.convs, key)
		m.saveProjectSnapshotLocked(key)
		return nil, false
	}

	// Check token budget
	if m.maxTokens > 0 && c.TotalTokens >= m.maxTokens {
		log.Printf("%s: project %s reached conversation token budget (%d/%d tokens) — rotating",
			m.adapterID, key, c.TotalTokens, m.maxTokens)
		delete(m.convs, key)
		m.saveProjectSnapshotLocked(key)
		return nil, false
	}

	// Check TTL
	if m.ttl > 0 && time.Since(c.LastUsedAt) > m.ttl {
		log.Printf("%s: project %s conversation expired (idle %v) — rotating",
			m.adapterID, key, time.Since(c.LastUsedAt).Round(time.Minute))
		delete(m.convs, key)
		m.saveProjectSnapshotLocked(key)
		return nil, false
	}

	c.LastUsedAt = time.Now()
	m.saveProjectSnapshotLocked(key)

	// Return a copy to avoid data races
	cp := *c
	return &cp, true
}

// Register stores or updates an active conversation for a project.
func (m *ProjectConversationManager) Register(project, sessionID, convID, parentID string, meta []string) {
	m.RegisterTurn(project, sessionID, convID, parentID, meta, HistoryMark{})
}

// RegisterTurn is Register that also checkpoints the client history the
// turn answered (see ProjectConversation.ForkPoint).
func (m *ProjectConversationManager) RegisterTurn(project, sessionID, convID, parentID string, meta []string, mark HistoryMark) {
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
	if mark.Len > 0 {
		c := m.convs[key]
		// Checkpoints at or past this history belong to a branch this turn
		// replaces (a side request, or the turn before a rewind).
		kept := make([]ThreadCheckpoint, 0, len(c.Checkpoints)+1)
		for _, cp := range c.Checkpoints {
			if cp.Mark.Len < mark.Len {
				kept = append(kept, cp)
			}
		}
		kept = append(kept, ThreadCheckpoint{Mark: mark, ParentID: parentID})
		if len(kept) > maxThreadCheckpoints {
			kept = kept[len(kept)-maxThreadCheckpoints:]
		}
		c.Checkpoints = kept
	}
	m.saveProjectSnapshotLocked(key)
}

// RecordTokens accumulates token consumption on the active conversation for a project.
func (m *ProjectConversationManager) RecordTokens(project string, tokens int) {
	if tokens <= 0 {
		return
	}
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.convs[key]; ok && c != nil {
		c.TotalTokens += tokens
		c.LastUsedAt = time.Now()
		m.saveProjectSnapshotLocked(key)
	}
}

// ResetProject removes a conversation thread. Given a thread key it clears
// that thread; given a plain project it clears every session thread of the
// project, including snapshots not loaded since the gateway started.
func (m *ProjectConversationManager) ResetProject(project string) {
	key := NormalizeProjectKey(project)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.convs, key)
	m.saveProjectSnapshotLocked(key)
	if _, thread := splitThreadKey(key); thread == "" {
		for k := range m.convs {
			if strings.HasPrefix(k, key+threadSep) {
				delete(m.convs, k)
			}
		}
		pattern := filepath.Join(types.ProjectCacheDir(key), "conv_"+m.snapshotID()+"_*.json")
		if files, err := filepath.Glob(pattern); err == nil {
			for _, f := range files {
				_ = os.Remove(f)
			}
		}
	}
	log.Printf("%s: cleared conversation for project %s", m.adapterID, key)
}

// ResetAll clears all project conversation threads on this adapter.
func (m *ProjectConversationManager) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.convs {
		m.convs[k] = nil
		m.saveProjectSnapshotLocked(k)
	}
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
