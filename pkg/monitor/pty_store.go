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

// PTYStoreRotationConfig defines size and retention thresholds for rotating session store files.
type PTYStoreRotationConfig struct {
	MaxSizeBytes int64         // Max size in bytes before rotating (default: 5MB)
	MaxBackups   int           // Max number of rotated backup files to retain (default: 5)
	MaxAge       time.Duration // Max age for stale dead sessions before pruning (default: 7 days)
	AutoRotate   bool          // Whether to auto-rotate during SaveAll (default: true)
}

// DefaultPTYStoreRotationConfig returns default rotation settings.
func DefaultPTYStoreRotationConfig() PTYStoreRotationConfig {
	return PTYStoreRotationConfig{
		MaxSizeBytes: 5 * 1024 * 1024, // 5MB
		MaxBackups:   5,
		MaxAge:       7 * 24 * time.Hour,
		AutoRotate:   true,
	}
}

// PTYSessionStore provides atomic, crash-resilient persistence for PTY session states
// with automatic file rotation and retention management.
type PTYSessionStore struct {
	mu             sync.RWMutex
	path           string
	rotationConfig PTYStoreRotationConfig
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
	return NewPTYSessionStoreWithConfig(path, DefaultPTYStoreRotationConfig())
}

// NewPTYSessionStoreWithConfig creates a persistence store with custom rotation settings.
func NewPTYSessionStoreWithConfig(path string, config PTYStoreRotationConfig) *PTYSessionStore {
	if path == "" {
		path = DefaultPTYStorePath()
	}
	if config.MaxSizeBytes <= 0 {
		config.MaxSizeBytes = 5 * 1024 * 1024
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = 5
	}
	if config.MaxAge <= 0 {
		config.MaxAge = 7 * 24 * time.Hour
	}
	return &PTYSessionStore{
		path:           path,
		rotationConfig: config,
	}
}

// SetRotationConfig updates the rotation configuration for this store.
func (s *PTYSessionStore) SetRotationConfig(config PTYStoreRotationConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotationConfig = config
}

// SaveAll atomically writes all active session states to disk, rotating if size limit is exceeded.
func (s *PTYSessionStore) SaveAll(sessions map[string]*PTYSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if current persistence file exceeds max size threshold
	if s.rotationConfig.AutoRotate {
		if fi, err := os.Stat(s.path); err == nil {
			if fi.Size() >= s.rotationConfig.MaxSizeBytes {
				_ = s.rotateLocked()
			}
		}
	}

	cutoff := time.Now().Add(-s.rotationConfig.MaxAge)
	data := sessionStoreData{
		UpdatedAt: time.Now(),
		Sessions:  make(map[string]PTYSession, len(sessions)),
	}
	for id, sess := range sessions {
		if sess != nil {
			// Skip dead sessions older than MaxAge to prevent unbounded growth
			if sess.State == PTYStateDead && !sess.LastHeartbeat.IsZero() && sess.LastHeartbeat.Before(cutoff) {
				continue
			}
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

// Rotate triggers an explicit file rotation on the current persistence file.
func (s *PTYSessionStore) Rotate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rotateLocked()
}

// rotateLocked rotates the store file and cascades backup numbers up to MaxBackups.
func (s *PTYSessionStore) rotateLocked() error {
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return nil
	}

	maxBackups := s.rotationConfig.MaxBackups
	if maxBackups < 1 {
		maxBackups = 1
	}

	// Remove oldest backup if it exists (e.g. .5)
	oldestBackup := fmt.Sprintf("%s.%d", s.path, maxBackups)
	_ = os.Remove(oldestBackup)

	// Cascade rotate existing backups down (e.g. .4 -> .5, .3 -> .4, .2 -> .3, .1 -> .2)
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", s.path, i)
		dst := fmt.Sprintf("%s.%d", s.path, i+1)
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}

	// Rotate current file to .1
	firstBackup := fmt.Sprintf("%s.1", s.path)
	if err := os.Rename(s.path, firstBackup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate pty store file: %w", err)
	}

	AppendEvent("pty:rotated", fmt.Sprintf("rotated %s to %s", s.path, firstBackup))
	return nil
}

// PruneDeadSessions removes dead session records exceeding maxAge from persistence.
func (s *PTYSessionStore) PruneDeadSessions(maxAge time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read pty store for prune: %w", err)
	}

	var data sessionStoreData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return 0, fmt.Errorf("unmarshal pty store for prune: %w", err)
	}

	if data.Sessions == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-maxAge)
	prunedCount := 0
	for id, sess := range data.Sessions {
		if (sess.State == PTYStateDead || !isProcessAlive(sess.PID)) && !sess.LastHeartbeat.IsZero() && sess.LastHeartbeat.Before(cutoff) {
			delete(data.Sessions, id)
			prunedCount++
		}
	}

	if prunedCount > 0 {
		newBytes, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return 0, err
		}
		tmpPath := fmt.Sprintf("%s.tmp.%d", s.path, time.Now().UnixNano())
		if err := os.WriteFile(tmpPath, newBytes, 0o600); err != nil {
			return 0, err
		}
		if err := os.Rename(tmpPath, s.path); err != nil {
			_ = os.Remove(tmpPath)
			return 0, err
		}
		AppendEvent("pty:pruned_sessions", fmt.Sprintf("pruned %d dead sessions older than %v", prunedCount, maxAge))
	}

	return prunedCount, nil
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
