package provider

import (
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func threadReq(session, system string) *types.ChatRequest {
	return &types.ChatRequest{
		SessionID: session,
		Metadata:  map[string]any{"project": "/w/amux"},
		Messages: []types.ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: "hi"},
		},
	}
}

func TestThreadKey_SessionAndSystemPrompt(t *testing.T) {
	main := ThreadKey(threadReq("s1", "You are Claude Code."))
	if again := ThreadKey(threadReq("s1", "You are Claude Code.")); again != main {
		t.Fatalf("same session and system must share a thread: %q vs %q", main, again)
	}
	if other := ThreadKey(threadReq("s2", "You are Claude Code.")); other == main {
		t.Fatal("a new client session must get its own thread")
	}
	if title := ThreadKey(threadReq("s1", "Generate a short title.")); title == main {
		t.Fatal("a side request with another system prompt must not share the main thread")
	}
}

func TestThreadKey_IgnoresBillingHeader(t *testing.T) {
	a := ThreadKey(threadReq("s1", "x-anthropic-billing-header: cc_version=2.1.1; cch=aaaa;\nYou are Claude Code."))
	b := ThreadKey(threadReq("s1", "x-anthropic-billing-header: cc_version=2.1.1; cch=bbbb;\nYou are Claude Code."))
	if a != b {
		t.Fatalf("per-request billing header must not split the thread: %q vs %q", a, b)
	}
}

func TestProjectConversationManager_SessionThreadsPersistAndResetByProject(t *testing.T) {
	t.Setenv("AM_DIR", t.TempDir())
	keyA := ThreadKey(threadReq("s1", "sys"))
	keyB := ThreadKey(threadReq("s2", "sys"))

	mgr := NewProjectConversationManager("chatgpt:test", 25, time.Hour)
	mgr.Register(keyA, "s1", "conv-a", "p-a", nil)
	mgr.Register(keyB, "s2", "conv-b", "p-b", nil)

	// A restarted gateway reloads each session's thread from its own snapshot.
	reloaded := NewProjectConversationManager("chatgpt:test", 25, time.Hour)
	for key, want := range map[string]string{keyA: "conv-a", keyB: "conv-b"} {
		if c, ok := reloaded.GetActive(key); !ok || c.ID != want {
			t.Fatalf("thread %q: want %s, got %+v", key, want, c)
		}
	}

	reloaded.ResetProject("/w/amux")
	fresh := NewProjectConversationManager("chatgpt:test", 25, time.Hour)
	for _, key := range []string{keyA, keyB} {
		if c, ok := fresh.GetActive(key); ok {
			t.Fatalf("project reset left thread %q: %+v", key, c)
		}
	}
}
