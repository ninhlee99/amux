package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPTYSessionStore_AtomicSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test_pty_sessions.json")
	store := NewPTYSessionStore(storePath)

	sessions := map[string]*PTYSession{
		"term-1": {
			SessionID:     "term-1",
			PID:           os.Getpid(),
			TTY:           "/dev/pts/1",
			Command:       "claude",
			State:         PTYStateActive,
			CreatedAt:     time.Now().Add(-10 * time.Minute),
			LastHeartbeat: time.Now(),
		},
		"term-2": {
			SessionID:     "term-2",
			PID:           99999999, // dead PID
			TTY:           "/dev/pts/2",
			Command:       "tmux",
			State:         PTYStateStale,
			CreatedAt:     time.Now().Add(-30 * time.Minute),
			LastHeartbeat: time.Now().Add(-5 * time.Minute),
		},
	}

	if err := store.SaveAll(sessions); err != nil {
		t.Fatalf("SaveAll failed: %v", err)
	}

	loaded, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded sessions, got %d", len(loaded))
	}
	if loaded["term-1"].Command != "claude" {
		t.Errorf("expected command claude, got %s", loaded["term-1"].Command)
	}

	// Test FindRecoverableSessions
	recoverable, dead, err := store.FindRecoverableSessions()
	if err != nil {
		t.Fatalf("FindRecoverableSessions failed: %v", err)
	}
	if len(recoverable) != 1 || recoverable[0].SessionID != "term-1" {
		t.Errorf("expected 1 recoverable session (term-1), got %+v", recoverable)
	}
	if len(dead) != 1 || dead[0].SessionID != "term-2" {
		t.Errorf("expected 1 dead session (term-2), got %+v", dead)
	}

	// Test ReconnectSession
	reconnected, err := store.ReconnectSession("term-1")
	if err != nil {
		t.Fatalf("ReconnectSession failed for alive process: %v", err)
	}
	if reconnected.State != PTYStateActive {
		t.Errorf("expected state active after reconnect, got %s", reconnected.State)
	}

	// Test Reconnect failure on dead session
	_, err = store.ReconnectSession("term-2")
	if err == nil {
		t.Fatal("expected reconnect error for dead process, got nil")
	}
}

func TestPTYHeartbeatMonitor_DaemonCrashRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test_recovery_sessions.json")

	cfg1 := PTYHeartbeatConfig{
		Interval:  10 * time.Millisecond,
		StorePath: storePath,
	}

	m1 := NewPTYHeartbeatMonitor(cfg1)
	m1.Register(PTYSession{
		SessionID: "survivor-sess",
		PID:       os.Getpid(),
		Command:   "claude code --resume",
	})
	m1.Register(PTYSession{
		SessionID: "dead-sess",
		PID:       99999999,
		Command:   "bash",
	})

	// Daemon 1 crashes / stops
	m1.Stop()

	// New daemon starts up with same persistence store
	cfg2 := PTYHeartbeatConfig{
		Interval:  10 * time.Millisecond,
		StorePath: storePath,
	}
	m2 := NewPTYHeartbeatMonitor(cfg2)
	recovered, err := m2.RecoverFromStore()
	if err != nil {
		t.Fatalf("RecoverFromStore failed: %v", err)
	}
	if len(recovered) != 1 || recovered[0].SessionID != "survivor-sess" {
		t.Fatalf("expected survivor-sess to be restored, got %+v", recovered)
	}

	// Verify user can reconnect
	sess, err := m2.Reconnect("survivor-sess")
	if err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}
	if sess.State != PTYStateActive {
		t.Errorf("expected active state, got %s", sess.State)
	}
}
