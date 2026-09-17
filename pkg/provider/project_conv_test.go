package provider

import (
	"testing"
	"time"
)

func TestProjectConversationManager_Isolation(t *testing.T) {
	t.Setenv("AM_DIR", t.TempDir())
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
	t.Setenv("AM_DIR", t.TempDir())
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

func TestProjectConversationManager_TokenBudgetRotation(t *testing.T) {
	t.Setenv("AM_DIR", t.TempDir())
	mgr := NewProjectConversationManager("test-token-budget", 50, 1*time.Hour)
	mgr.maxTokens = 5000 // Test with small budget

	proj := "/workspace/budget-project"
	mgr.Register(proj, "s1", "conv-token-1", "p1", nil)

	c, ok := mgr.GetActive(proj)
	if !ok || c.ID != "conv-token-1" {
		t.Fatalf("expected active conversation conv-token-1")
	}

	// Record 3000 tokens (still below 5000)
	mgr.RecordTokens(proj, 3000)
	c, ok = mgr.GetActive(proj)
	if !ok || c.TotalTokens != 3000 {
		t.Fatalf("expected active conversation with 3000 tokens, got %v", c)
	}

	// Record another 2500 tokens (total 5500, exceeds 5000 budget)
	mgr.RecordTokens(proj, 2500)

	// Next GetActive must rotate
	_, ok = mgr.GetActive(proj)
	if ok {
		t.Fatalf("expected conversation to rotate after exceeding token budget")
	}
}

func TestProjectConversationManager_SnapshotPersistence(t *testing.T) {
	t.Setenv("AM_DIR", t.TempDir())
	proj := "/workspace/persistence-project"
	mgr1 := NewProjectConversationManager("test-persist-adapter", 25, 1*time.Hour)
	mgr1.Register(proj, "sess-snap", "conv-snap-123", "parent-456", []string{"meta1", "meta2"})
	mgr1.RecordTokens(proj, 1200)

	// Verify manager 1 has it
	c1, ok := mgr1.GetActive(proj)
	if !ok || c1.ID != "conv-snap-123" || c1.TotalTokens != 1200 {
		t.Fatalf("expected conv-snap-123 with 1200 tokens in mgr1, got %+v", c1)
	}

	// Create manager 2 from scratch (simulating proxy restart)
	mgr2 := NewProjectConversationManager("test-persist-adapter", 25, 1*time.Hour)
	// In-memory map is empty
	if len(mgr2.convs) != 0 {
		t.Fatalf("expected mgr2 convs map to be initially empty")
	}

	// GetActive on mgr2 should load snapshot from disk
	c2, ok := mgr2.GetActive(proj)
	if !ok || c2 == nil {
		t.Fatalf("expected mgr2 to restore active conversation from snapshot")
	}
	if c2.ID != "conv-snap-123" {
		t.Errorf("expected ID conv-snap-123, got %s", c2.ID)
	}
	if c2.ParentID != "parent-456" {
		t.Errorf("expected ParentID parent-456, got %s", c2.ParentID)
	}
	if c2.TotalTokens != 1200 {
		t.Errorf("expected TotalTokens 1200, got %d", c2.TotalTokens)
	}
	if len(c2.Metadata) != 2 || c2.Metadata[0] != "meta1" {
		t.Errorf("expected metadata restored, got %v", c2.Metadata)
	}

	// Reset should delete snapshot from disk
	mgr2.ResetProject(proj)
	snapFile := mgr2.snapshotPath(NormalizeProjectKey(proj))
	c3, found := mgr2.loadProjectSnapshotLocked(NormalizeProjectKey(proj))
	if found || c3 != nil {
		t.Errorf("expected snapshot file %s to be removed after ResetProject", snapFile)
	}
}

func TestProjectConversationManager_Reset(t *testing.T) {
	t.Setenv("AM_DIR", t.TempDir())
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
