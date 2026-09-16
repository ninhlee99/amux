package provider

import (
	"testing"
	"time"
)

func TestProjectConversationManager_Isolation(t *testing.T) {
	mgr := NewProjectConversationManager("test-claude-web", 25, 1*time.Hour)

	projA := "/Users/developer/apps/projectA"
	projB := "/Users/developer/apps/projectB"

	// Initial check: no active conversation for either project
	if _, ok := mgr.GetActive(projA); ok {
		t.Fatalf("expected no active conversation for project A initially")
	}
	if _, ok := mgr.GetActive(projB); ok {
		t.Fatalf("expected no active conversation for project B initially")
	}

	// Register conversation for Project A
	mgr.Register(projA, "sess_1", "conv_uuid_aaa", "parent_1", nil)

	// Verify Project A has active conversation
	c1, ok := mgr.GetActive(projA)
	if !ok || c1 == nil || c1.ID != "conv_uuid_aaa" {
		t.Fatalf("expected conv_uuid_aaa for project A, got %+v", c1)
	}

	// Verify Project B is strictly isolated and gets NOTHING
	if c2, ok := mgr.GetActive(projB); ok {
		t.Fatalf("expected project B to have NO active conversation, got %+v", c2)
	}

	// Now register conversation for Project B
	mgr.Register(projB, "sess_2", "conv_uuid_bbb", "parent_2", nil)

	// Verify both projects maintain their own distinct conversation IDs
	cA, okA := mgr.GetActive(projA)
	cB, okB := mgr.GetActive(projB)

	if !okA || cA.ID != "conv_uuid_aaa" {
		t.Errorf("expected conv_uuid_aaa for project A, got %v", cA)
	}
	if !okB || cB.ID != "conv_uuid_bbb" {
		t.Errorf("expected conv_uuid_bbb for project B, got %v", cB)
	}
}

func TestProjectConversationManager_TurnLimitRotation(t *testing.T) {
	maxTurns := 3
	mgr := NewProjectConversationManager("test-adapter", maxTurns, 1*time.Hour)

	proj := "/workspace/my-project"

	// Turn 1: register response, count becomes 1
	mgr.Register(proj, "s1", "conv-1", "p1", nil)
	c, ok := mgr.GetActive(proj)
	if !ok || c.TurnCount != 1 {
		t.Fatalf("turn 1: expected active with turn count 1, got %v", c)
	}

	// Turn 2: register response, count becomes 2
	mgr.Register(proj, "s1", "conv-1", "p2", nil)
	c, ok = mgr.GetActive(proj)
	if !ok || c.TurnCount != 2 {
		t.Fatalf("turn 2: expected active with turn count 2, got %v", c)
	}

	// Turn 3: register response, count becomes 3 (maxTurns completed)
	mgr.Register(proj, "s1", "conv-1", "p3", nil)

	// Turn 4 request attempt: reached maxTurns (3) -> GetActive must rotate and return false
	_, ok = mgr.GetActive(proj)
	if ok {
		t.Fatalf("expected turn limit rotation to return false on turn 4")
	}

	// Registering a new conversation starts fresh at turn 1
	mgr.Register(proj, "s1", "conv-2", "p-fresh", nil)
	cFresh, ok := mgr.GetActive(proj)
	if !ok || cFresh.ID != "conv-2" || cFresh.TurnCount != 1 {
		t.Fatalf("expected fresh conversation conv-2 with turn 1, got %+v", cFresh)
	}
}

func TestProjectConversationManager_Reset(t *testing.T) {
	mgr := NewProjectConversationManager("test-adapter", 25, 1*time.Hour)

	proj1 := "/workspace/proj1"
	proj2 := "/workspace/proj2"

	mgr.Register(proj1, "s1", "c1", "", nil)
	mgr.Register(proj2, "s2", "c2", "", nil)

	// Reset only proj1
	mgr.ResetProject(proj1)

	if _, ok := mgr.GetActive(proj1); ok {
		t.Errorf("expected proj1 to be cleared after ResetProject")
	}
	if _, ok := mgr.GetActive(proj2); !ok {
		t.Errorf("expected proj2 to remain active after ResetProject(proj1)")
	}

	// ResetAll clears everything
	mgr.ResetAll()
	if _, ok := mgr.GetActive(proj2); ok {
		t.Errorf("expected proj2 to be cleared after ResetAll")
	}
}
