package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

// PTYSessionStore provides atomic, crash-resilient persistence for PTY session states.
type PTYSessionStore struct {
	mu   sync.RWMutex
	path string
}

type sessionStoreData struct {
	UpdatedAt time.Time             `json:"updated_at"`
	Sessions  map[string]PTYSession `json:"sessions"`
}

// DefaultPTYStorePath returns the canonical file path for local session persistence.
func DefaultPTYStorePath() string {
	return filepath.Join(types.BaseDir(), "pty_sessions.json")
}

// NewPTYSessionStore creates a persistence store with the given path (or default if empty).
func NewPTYSessionStore(path string) *PTYSessionStore {
	if path == "" {
		path = DefaultPTYStorePath()
	}
	return &PTYSessionStore{path: path}
}

// SaveAll atomically writes all active session states to disk.
func (s *PTYSessionStore) SaveAll(sessions map[string]*PTYSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := sessionStoreData{
		UpdatedAt: time.Now(),
		Sessions:  make(map[string]PTYSession, len(sessions)),
	}
	for id, sess := range sessions {
		if sess != nil {
			data.Sessions[id] = *sess
		}
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pty sessions: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create pty store dir: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", s.path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, bytes, 0o600); err != nil {
		return fmt.Errorf("write tmp pty session file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename pty session file: %w", err)
	}

	return nil
}

// LoadAll reads and deserializes all persisted sessions from disk.
func (s *PTYSessionStore) LoadAll() (map[string]PTYSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]PTYSession), nil
		}
		return nil, fmt.Errorf("read pty store: %w", err)
	}

	var data sessionStoreData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return nil, fmt.Errorf("unmarshal pty sessions: %w", err)
	}

	if data.Sessions == nil {
		data.Sessions = make(map[string]PTYSession)
	}

	return data.Sessions, nil
}

// FindRecoverableSessions scans persisted session records and identifies processes
// that are still alive in the OS, ready to be reattached even after a daemon crash.
func (s *PTYSessionStore) FindRecoverableSessions() ([]PTYSession, []PTYSession, error) {
	persisted, err := s.LoadAll()
	if err != nil {
		return nil, nil, err
	}

	var recoverable []PTYSession
	var dead []PTYSession

	for _, sess := range persisted {
		if sess.PID > 0 && isProcessAlive(sess.PID) {
			recoverable = append(recoverable, sess)
		} else {
			dead = append(dead, sess)
		}
	}

	return recoverable, dead, nil
}

// ReconnectSession validates that the specified session process is still alive and returns it.
func (s *PTYSessionStore) ReconnectSession(sessionID string) (*PTYSession, error) {
	sessions, err := s.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load sessions for reconnect: %w", err)
	}

	sess, ok := sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %q not found in persistent store", sessionID)
	}

	if sess.PID <= 0 || !isProcessAlive(sess.PID) {
		return nil, fmt.Errorf("session %q process (PID %d) is no longer running", sessionID, sess.PID)
	}

	sess.State = PTYStateActive
	sess.LastHeartbeat = time.Now()
	return &sess, nil
}
