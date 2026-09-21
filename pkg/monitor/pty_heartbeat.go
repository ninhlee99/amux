package monitor

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// PTYSessionState denotes the health state of an active terminal process.
type PTYSessionState string

const (
	PTYStateActive PTYSessionState = "active"
	PTYStateIdle   PTYSessionState = "idle"
	PTYStateStale  PTYSessionState = "stale"
	PTYStateHung   PTYSessionState = "hung"
	PTYStateDead   PTYSessionState = "dead"
)

// PTYSession represents a monitored terminal session / pseudo-terminal process.
type PTYSession struct {
	SessionID     string            `json:"session_id"`
	PID           int               `json:"pid"`
	TTY           string            `json:"tty,omitempty"`
	Command       string            `json:"command,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	State         PTYSessionState   `json:"state"`
	CreatedAt     time.Time         `json:"created_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	LastIOBytes   int64             `json:"last_io_bytes"`
	MissedBeats   int               `json:"missed_beats"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// PTYHeartbeatConfig defines configuration parameters for heartbeat monitoring.
type PTYHeartbeatConfig struct {
	Interval       time.Duration
	StaleThreshold time.Duration
	HungThreshold  time.Duration
	AutoKillDead   bool
	StorePath      string
	OnStale        func(sess *PTYSession)
	OnHung         func(sess *PTYSession)
	OnDead         func(sess *PTYSession)
}

// DefaultPTYHeartbeatConfig returns production defaults.
func DefaultPTYHeartbeatConfig() PTYHeartbeatConfig {
	return PTYHeartbeatConfig{
		Interval:       10 * time.Second,
		StaleThreshold: 2 * time.Minute,
		HungThreshold:  5 * time.Minute,
		AutoKillDead:   true,
		StorePath:      DefaultPTYStorePath(),
	}
}

// PTYHeartbeatMonitor monitors active terminal and PTY processes in the background,
// identifying stale or hung sessions and reporting health diagnostics.
type PTYHeartbeatMonitor struct {
	mu       sync.RWMutex
	cfg      PTYHeartbeatConfig
	sessions map[string]*PTYSession
	store    *PTYSessionStore
	stopCh   chan struct{}
	running  atomic.Bool
	closed   atomic.Bool
}

var (
	globalPTYMonitor     *PTYHeartbeatMonitor
	globalPTYMonitorOnce sync.Once
)

// GlobalPTYMonitor returns the singleton PTY heartbeat monitor.
func GlobalPTYMonitor() *PTYHeartbeatMonitor {
	globalPTYMonitorOnce.Do(func() {
		globalPTYMonitor = NewPTYHeartbeatMonitor(DefaultPTYHeartbeatConfig())
		_, _ = globalPTYMonitor.RecoverFromStore()
		globalPTYMonitor.Start()
	})
	return globalPTYMonitor
}

// NewPTYHeartbeatMonitor creates a new heartbeat monitor instance.
func NewPTYHeartbeatMonitor(cfg PTYHeartbeatConfig) *PTYHeartbeatMonitor {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.StaleThreshold <= 0 {
		cfg.StaleThreshold = 2 * time.Minute
	}
	if cfg.HungThreshold <= 0 {
		cfg.HungThreshold = 5 * time.Minute
	}
	if cfg.StorePath == "" {
		cfg.StorePath = DefaultPTYStorePath()
	}

	return &PTYHeartbeatMonitor{
		cfg:      cfg,
		sessions: make(map[string]*PTYSession),
		store:    NewPTYSessionStore(cfg.StorePath),
		stopCh:   make(chan struct{}),
	}
}

// RecoverFromStore restores living PTY processes from disk after a daemon restart or crash.
func (m *PTYHeartbeatMonitor) RecoverFromStore() ([]PTYSession, error) {
	recoverable, dead, err := m.store.FindRecoverableSessions()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range recoverable {
		sessCopy := s
		sessCopy.State = PTYStateActive
		sessCopy.LastHeartbeat = time.Now()
		m.sessions[sessCopy.SessionID] = &sessCopy
		AppendEvent("pty:recovered_from_disk", fmt.Sprintf("session=%s pid=%d recovered from persistent store", sessCopy.SessionID, sessCopy.PID))
	}

	for _, s := range dead {
		AppendEvent("pty:pruned_dead_disk", fmt.Sprintf("session=%s pid=%d pruned dead process from store", s.SessionID, s.PID))
	}

	// Update store with active states
	_ = m.store.SaveAll(m.sessions)
	return recoverable, nil
}

// Reconnect allows a user or CLI command to re-attach to an active terminal session.
func (m *PTYHeartbeatMonitor) Reconnect(sessionID string) (*PTYSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check in-memory first
	if sess, ok := m.sessions[sessionID]; ok {
		if sess.PID > 0 && !isProcessAlive(sess.PID) {
			sess.State = PTYStateDead
			return nil, fmt.Errorf("session %q process (PID %d) is dead", sessionID, sess.PID)
		}
		sess.State = PTYStateActive
		sess.LastHeartbeat = time.Now()
		_ = m.store.SaveAll(m.sessions)
		return sess, nil
	}

	// Fallback to disk store
	sess, err := m.store.ReconnectSession(sessionID)
	if err != nil {
		return nil, err
	}

	m.sessions[sess.SessionID] = sess
	_ = m.store.SaveAll(m.sessions)
	return sess, nil
}

// Start kicks off the background heartbeat monitoring goroutine.
func (m *PTYHeartbeatMonitor) Start() {
	if m.running.CompareAndSwap(false, true) {
		go m.monitorLoop()
	}
}

// Stop gracefully stops the monitoring loop.
func (m *PTYHeartbeatMonitor) Stop() {
	if m.running.CompareAndSwap(true, false) {
		close(m.stopCh)
	}
}

// Register registers a new active terminal/PTY session for heartbeat monitoring.
func (m *PTYHeartbeatMonitor) Register(sess PTYSession) {
	if sess.SessionID == "" {
		return
	}
	now := time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.LastHeartbeat.IsZero() {
		sess.LastHeartbeat = now
	}
	if sess.State == "" {
		sess.State = PTYStateActive
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.SessionID] = &sess
	_ = m.store.SaveAll(m.sessions)

	AppendEvent("pty:register", fmt.Sprintf("session=%s pid=%d cmd=%q", sess.SessionID, sess.PID, sess.Command))
}

// Unregister removes a session from monitoring upon clean exit.
func (m *PTYHeartbeatMonitor) Unregister(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, exists := m.sessions[sessionID]; exists {
		delete(m.sessions, sessionID)
		_ = m.store.SaveAll(m.sessions)
		AppendEvent("pty:unregister", fmt.Sprintf("session=%s pid=%d", sessionID, sess.PID))
	}
}

// Heartbeat updates the last heartbeat time and resets the missed beats counter.
func (m *PTYHeartbeatMonitor) Heartbeat(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return false
	}

	sess.LastHeartbeat = time.Now()
	sess.MissedBeats = 0
	if sess.State == PTYStateStale || sess.State == PTYStateHung {
		sess.State = PTYStateActive
		AppendEvent("pty:recovered", fmt.Sprintf("session=%s pid=%d recovered to active", sessionID, sess.PID))
	}
	return true
}

// RecordIO records active I/O transfer on a session, treating it as a healthy heartbeat.
func (m *PTYHeartbeatMonitor) RecordIO(sessionID string, bytes int) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, ok := m.sessions[sessionID]; ok {
		sess.LastHeartbeat = time.Now()
		sess.LastIOBytes += int64(bytes)
		sess.MissedBeats = 0
		sess.State = PTYStateActive
	}
}

// GetSession retrieves the status of a specific terminal session.
func (m *PTYHeartbeatMonitor) GetSession(sessionID string) (PTYSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return PTYSession{}, false
	}
	return *sess, true
}

// ListSessions returns a snapshot copy of all monitored sessions.
func (m *PTYHeartbeatMonitor) ListSessions() []PTYSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]PTYSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, *s)
	}
	return list
}

// GetStaleSessions returns all sessions currently flagged as stale.
func (m *PTYHeartbeatMonitor) GetStaleSessions() []PTYSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []PTYSession
	for _, s := range m.sessions {
		if s.State == PTYStateStale {
			result = append(result, *s)
		}
	}
	return result
}

// GetHungSessions returns all sessions currently flagged as hung.
func (m *PTYHeartbeatMonitor) GetHungSessions() []PTYSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []PTYSession
	for _, s := range m.sessions {
		if s.State == PTYStateHung {
			result = append(result, *s)
		}
	}
	return result
}

// monitorLoop periodically inspects all active sessions for staleness, hung state, or death.
func (m *PTYHeartbeatMonitor) monitorLoop() {
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkSessions()
		}
	}
}

// checkSessions performs health evaluation on each session.
func (m *PTYHeartbeatMonitor) checkSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, sess := range m.sessions {
		// 1. Check if underlying OS process is still alive if PID is set
		if sess.PID > 0 {
			if !isProcessAlive(sess.PID) {
				sess.State = PTYStateDead
				AppendEvent("pty:dead", fmt.Sprintf("session=%s pid=%d command=%q process terminated", sess.SessionID, sess.PID, sess.Command))
				if m.cfg.OnDead != nil {
					m.cfg.OnDead(sess)
				}
				continue
			}
		}

		// 2. Evaluate heartbeat elapsed time
		elapsed := now.Sub(sess.LastHeartbeat)
		sess.MissedBeats++

		if elapsed >= m.cfg.HungThreshold {
			if sess.State != PTYStateHung {
				sess.State = PTYStateHung
				AppendEvent("pty:hung", fmt.Sprintf("session=%s pid=%d hung for %v (command=%q)", sess.SessionID, sess.PID, elapsed.Round(time.Second), sess.Command))
				if m.cfg.OnHung != nil {
					m.cfg.OnHung(sess)
				}
			}
		} else if elapsed >= m.cfg.StaleThreshold {
			if sess.State != PTYStateStale && sess.State != PTYStateHung {
				sess.State = PTYStateStale
				AppendEvent("pty:stale", fmt.Sprintf("session=%s pid=%d inactive for %v", sess.SessionID, sess.PID, elapsed.Round(time.Second)))
				if m.cfg.OnStale != nil {
					m.cfg.OnStale(sess)
				}
			}
		}
	}
}

// isProcessAlive sends signal 0 to test process existence.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; Signal(syscall.Signal(0)) checks existence
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
