package monitor

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestPTYHeartbeatMonitor_Lifecycle(t *testing.T) {
	cfg := PTYHeartbeatConfig{
		Interval:       10 * time.Millisecond,
		StaleThreshold: 50 * time.Millisecond,
		HungThreshold:  120 * time.Millisecond,
	}

	var staleCalled, hungCalled atomic.Bool
	cfg.OnStale = func(s *PTYSession) {
		staleCalled.Store(true)
	}
	cfg.OnHung = func(s *PTYSession) {
		hungCalled.Store(true)
	}

	m := NewPTYHeartbeatMonitor(cfg)
	m.Start()
	defer m.Stop()

	// 1. Register session
	sessID := "term-mux-01"
	m.Register(PTYSession{
		SessionID: sessID,
		PID:       os.Getpid(), // current test process is alive
		Command:   "claude",
		TTY:       "/dev/pts/1",
	})

	sess, ok := m.GetSession(sessID)
	if !ok || sess.State != PTYStateActive {
		t.Fatalf("expected active session, got %+v", sess)
	}

	// 2. Wait to reach stale state
	staleOk := waitCondition(300*time.Millisecond, func() bool {
		staleList := m.GetStaleSessions()
		return len(staleList) == 1 && staleList[0].SessionID == sessID && staleCalled.Load()
	})
	if !staleOk {
		t.Errorf("expected 1 stale session and callback invoked, got staleList=%+v staleCalled=%v", m.GetStaleSessions(), staleCalled.Load())
	}

	// 3. Heartbeat recovery
	ok = m.Heartbeat(sessID)
	if !ok {
		t.Fatal("heartbeat failed for registered session")
	}
	sess, _ = m.GetSession(sessID)
	if sess.State != PTYStateActive {
		t.Errorf("expected session to recover to active after heartbeat, got %s", sess.State)
	}

	// 4. Wait to reach hung state
	hungOk := waitCondition(500*time.Millisecond, func() bool {
		hungList := m.GetHungSessions()
		return len(hungList) == 1 && hungList[0].SessionID == sessID && hungCalled.Load()
	})
	if !hungOk {
		t.Errorf("expected 1 hung session and callback invoked, got hungList=%+v hungCalled=%v", m.GetHungSessions(), hungCalled.Load())
	}

	// 5. Unregister
	m.Unregister(sessID)
	if _, found := m.GetSession(sessID); found {
		t.Error("expected session to be unregistered")
	}
}

func TestPTYHeartbeatMonitor_DeadProcessDetection(t *testing.T) {
	cfg := PTYHeartbeatConfig{
		Interval:       10 * time.Millisecond,
		StaleThreshold: time.Second,
		HungThreshold:  5 * time.Second,
	}

	var deadCalled atomic.Bool
	cfg.OnDead = func(s *PTYSession) {
		deadCalled.Store(true)
	}

	m := NewPTYHeartbeatMonitor(cfg)
	m.Start()
	defer m.Stop()

	// Register a non-existent PID (e.g. 99999999)
	m.Register(PTYSession{
		SessionID: "term-dead-01",
		PID:       99999999,
		Command:   "bash",
	})

	time.Sleep(40 * time.Millisecond)

	sess, ok := m.GetSession("term-dead-01")
	if !ok {
		t.Fatal("session not found")
	}
	if sess.State != PTYStateDead {
		t.Errorf("expected state %s, got %s", PTYStateDead, sess.State)
	}
	if !deadCalled.Load() {
		t.Error("expected OnDead callback to be triggered")
	}
}

func TestPTYHeartbeatMonitor_RecordIO(t *testing.T) {
	m := NewPTYHeartbeatMonitor(DefaultPTYHeartbeatConfig())
	sessID := "term-io-01"
	m.Register(PTYSession{
		SessionID: sessID,
		PID:       os.Getpid(),
		Command:   "tmux",
	})

	m.RecordIO(sessID, 1024)
	sess, _ := m.GetSession(sessID)
	if sess.LastIOBytes != 1024 {
		t.Errorf("expected 1024 IO bytes recorded, got %d", sess.LastIOBytes)
	}
}

func waitCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

