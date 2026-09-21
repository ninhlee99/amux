package monitor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"amux-accounts/pkg/guard"
)

func TestUDSServerAndClient_LifecycleAndActions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-uds-test-*")
	if err != nil {
		t.Fatalf("create tmpdir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "test-amux.sock")
	pool := guard.NewWorkerPool(4, 32)
	defer pool.Close()

	mon := NewPTYHeartbeatMonitor(PTYHeartbeatConfig{
		Interval:       50 * time.Millisecond,
		StaleThreshold: 200 * time.Millisecond,
		HungThreshold:  500 * time.Millisecond,
	})
	defer mon.Stop()

	server := NewUDSServer(sockPath, mon, pool)
	if err := server.Start(); err != nil {
		t.Fatalf("start uds server: %v", err)
	}
	defer server.Stop()

	// Wait for socket to bind
	time.Sleep(50 * time.Millisecond)

	client := NewUDSClient(sockPath)
	if !client.IsDaemonAvailable() {
		t.Fatalf("expected daemon to be available via socket %s", sockPath)
	}

	// 1. Start a real dummy child process for safe killing
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dummy process: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	currentPID := cmd.Process.Pid
	mon.Register(PTYSession{
		SessionID: "test-sess-01",
		PID:       currentPID,
		Command:   "claude code",
		TTY:       "/dev/pts/1",
		State:     PTYStateActive,
	})
	sess, ok := mon.GetSession("test-sess-01")
	if !ok {
		t.Fatalf("session not found after register")
	}
	if sess.SessionID != "test-sess-01" {
		t.Errorf("expected session_id test-sess-01, got %s", sess.SessionID)
	}

	// Run a worker pool task tagged with session
	done := make(chan bool)
	pool.SubmitSession("test-sess-01", func() {
		defer close(done)
		time.Sleep(10 * time.Millisecond)
	})
	<-done

	// 2. Query Top telemetry via UDS
	topData, err := client.QueryTop()
	if err != nil {
		t.Fatalf("QueryTop failed: %v", err)
	}
	if topData.TotalSessions != 1 {
		t.Errorf("expected 1 total session, got %d", topData.TotalSessions)
	}
	if len(topData.Sessions) != 1 {
		t.Fatalf("expected 1 session in telemetry list, got %d", len(topData.Sessions))
	}
	if topData.Sessions[0].SessionID != "test-sess-01" {
		t.Errorf("expected session_id test-sess-01, got %s", topData.Sessions[0].SessionID)
	}
	if topData.Sessions[0].CompletedTasks != 1 {
		t.Errorf("expected 1 completed worker task, got %d", topData.Sessions[0].CompletedTasks)
	}

	// 3. Query Sessions list via UDS
	sessions, err := client.QuerySessions()
	if err != nil {
		t.Fatalf("QuerySessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session from QuerySessions, got %d", len(sessions))
	}

	// 4. Reconnect Session via UDS
	reconnected, err := client.ReconnectSession("test-sess-01")
	if err != nil {
		t.Fatalf("ReconnectSession failed: %v", err)
	}
	if reconnected.SessionID != "test-sess-01" {
		t.Errorf("expected reconnected session_id test-sess-01, got %s", reconnected.SessionID)
	}

	// 5. Kill / Unregister Session via UDS
	if err := client.KillSession("test-sess-01"); err != nil {
		t.Fatalf("KillSession failed: %v", err)
	}

	remaining, _ := client.QuerySessions()
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining sessions after kill, got %d", len(remaining))
	}
}
