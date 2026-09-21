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

func TestPTYSessionStore_RotationCascadeAndMaxBackups(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test_rot_sessions.json")

	rotCfg := PTYStoreRotationConfig{
		MaxSizeBytes: 200, // Very small to trigger rotation easily
		MaxBackups:   3,
		MaxAge:       24 * time.Hour,
		AutoRotate:   true,
	}
	store := NewPTYSessionStoreWithConfig(storePath, rotCfg)

	sess := map[string]*PTYSession{
		"sess-rot-1": {
			SessionID:     "sess-rot-1",
			PID:           os.Getpid(),
			Command:       "gemini-cli long session test command",
			State:         PTYStateActive,
			CreatedAt:     time.Now(),
			LastHeartbeat: time.Now(),
		},
	}

	// 1. First save creates main file
	if err := store.SaveAll(sess); err != nil {
		t.Fatalf("first SaveAll failed: %v", err)
	}

	// 2. Explicit rotate creates .1
	if err := store.Rotate(); err != nil {
		t.Fatalf("Rotate 1 failed: %v", err)
	}
	if _, err := os.Stat(storePath + ".1"); err != nil {
		t.Errorf("expected %s.1 to exist", storePath)
	}

	// 3. Save again and rotate again -> .1 becomes .2, new file becomes .1
	_ = store.SaveAll(sess)
	_ = store.Rotate()
	if _, err := os.Stat(storePath + ".2"); err != nil {
		t.Errorf("expected %s.2 to exist", storePath)
	}

	// 4. Rotate multiple times to test MaxBackups cap (max 3 backups)
	_ = store.SaveAll(sess)
	_ = store.Rotate()
	_ = store.SaveAll(sess)
	_ = store.Rotate()

	if _, err := os.Stat(storePath + ".3"); err != nil {
		t.Errorf("expected %s.3 to exist", storePath)
	}
	if _, err := os.Stat(storePath + ".4"); !os.IsNotExist(err) {
		t.Errorf("expected %s.4 to NOT exist because MaxBackups is 3", storePath)
	}
}

func TestPTYSessionStore_PruneDeadSessions(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test_prune_sessions.json")
	store := NewPTYSessionStore(storePath)

	oldTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago
	recentTime := time.Now().Add(-1 * time.Hour)

	sessions := map[string]*PTYSession{
		"alive-recent": {
			SessionID:     "alive-recent",
			PID:           os.Getpid(),
			State:         PTYStateActive,
			CreatedAt:     recentTime,
			LastHeartbeat: recentTime,
		},
		"dead-recent": {
			SessionID:     "dead-recent",
			PID:           99999999,
			State:         PTYStateDead,
			CreatedAt:     recentTime,
			LastHeartbeat: recentTime,
		},
		"dead-ancient": {
			SessionID:     "dead-ancient",
			PID:           99999998,
			State:         PTYStateDead,
			CreatedAt:     oldTime,
			LastHeartbeat: oldTime,
		},
	}

	// Configure store with large initial MaxAge so raw records are written to disk
	store.SetRotationConfig(PTYStoreRotationConfig{
		MaxAge: 30 * 24 * time.Hour,
	})
	_ = store.SaveAll(sessions)

	// Verify all 3 are on disk initially
	initial, _ := store.LoadAll()
	if len(initial) != 3 {
		t.Fatalf("expected 3 initial sessions on disk, got %d", len(initial))
	}

	// Prune sessions older than 7 days
	pruned, err := store.PruneDeadSessions(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneDeadSessions failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned session (dead-ancient), got %d", pruned)
	}

	loaded, _ := store.LoadAll()
	if _, exists := loaded["dead-ancient"]; exists {
		t.Error("expected dead-ancient to be removed")
	}
	if _, exists := loaded["dead-recent"]; !exists {
		t.Error("expected dead-recent to be retained")
	}
	if _, exists := loaded["alive-recent"]; !exists {
		t.Error("expected alive-recent to be retained")
	}
}
