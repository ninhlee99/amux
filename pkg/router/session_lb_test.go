package router

import (
	"context"
	"sync"
	"testing"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/types"
)

func TestRoleForGroup(t *testing.T) {
	cases := map[string]string{
		GroupCodexSub:   RoleCoding,
		GroupAGYSub:     RoleAnalysis,
		GroupClaudeWeb:  RoleReview,
		GroupChatGPTWeb: RoleCompact,
		GroupGeminiWeb:  RoleQuality,
		GroupAPIOther:   RoleAPI,
		GroupClaudeSub:  "",
	}
	for g, want := range cases {
		if got := RoleForGroup(g); got != want {
			t.Errorf("RoleForGroup(%s)=%q want %q", g, got, want)
		}
	}
}

func TestProxyGroupsForClient_ClaudeSkipsSubscription(t *testing.T) {
	groups := ProxyGroupsForClient(IDEClaude)
	for _, g := range groups {
		if IsClaudeSubscriptionGroup(g) {
			t.Fatalf("Claude IDE proxy order must not include %s: %v", g, groups)
		}
	}
	if groups[0] != GroupCodexSub {
		t.Fatalf("expected codex first for Claude IDE proxy, got %v", groups)
	}
}

func TestSessionLoadBalance_ThreeSessionsThreeAdapters(t *testing.T) {
	guard.GlobalAffinity().Unpin("sess-a")
	guard.GlobalAffinity().Unpin("sess-b")
	guard.GlobalAffinity().Unpin("sess-c")

	claudeWeb := &mockGroupAdapter{id: "claude:web:01", priority: 25, group: GroupClaudeWeb}
	chatgpt := &mockGroupAdapter{id: "chatgpt:01", priority: 20, group: GroupChatGPTWeb}
	geminiWeb := &mockGroupAdapter{id: "gemini:web:01", priority: 22, group: GroupGeminiWeb}
	// Claude subscription must never be picked as proxy
	claudeSub := &mockGroupAdapter{id: "claude:code:01", priority: 1, group: GroupClaudeSub}

	pool := NewAccountPoolRouter([]types.ProviderAdapter{claudeSub, claudeWeb, chatgpt, geminiWeb})
	// No manual pin — RR assigns new sessions.

	got := map[string]string{}
	for _, sid := range []string{"sess-a", "sess-b", "sess-c"} {
		ch, err := pool.Send(context.Background(), &types.ChatRequest{
			SessionID:     sid,
			ClientDialect: "claude",
			Messages:      []types.ChatMessage{{Role: "user", Content: "hi " + sid}},
		})
		if err != nil {
			t.Fatalf("session %s: %v", sid, err)
		}
		chunk := <-ch
		got[sid] = chunk.ID
		if chunk.ID == "claude:code:01" {
			t.Fatalf("session %s assigned Claude subscription proxy — forbidden", sid)
		}
	}

	uniq := map[string]bool{}
	for _, id := range got {
		uniq[id] = true
	}
	if len(uniq) != 3 {
		t.Fatalf("expected 3 different adapters for 3 sessions, got %v", got)
	}
}

func TestSessionLoadBalance_AffinityStickiness(t *testing.T) {
	guard.GlobalAffinity().Unpin("sticky-1")

	a := &mockGroupAdapter{id: "codex:01", priority: 1, group: GroupCodexSub}
	b := &mockGroupAdapter{id: "chatgpt:01", priority: 2, group: GroupChatGPTWeb}
	pool := NewAccountPoolRouter([]types.ProviderAdapter{a, b})

	req1 := &types.ChatRequest{
		SessionID:     "sticky-1",
		ClientDialect: "claude",
		Messages:      []types.ChatMessage{{Role: "user", Content: "turn1"}},
	}
	ch, err := pool.Send(context.Background(), req1)
	if err != nil {
		t.Fatal(err)
	}
	first := (<-ch).ID

	// Second turn same session — must stick even though RR would advance.
	req2 := &types.ChatRequest{
		SessionID:     "sticky-1",
		ClientDialect: "claude",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "turn1"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "turn2"},
		},
	}
	ch, err = pool.Send(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	second := (<-ch).ID
	if second != first {
		t.Fatalf("affinity broken: turn1=%s turn2=%s", first, second)
	}
	if a.callCount < 2 && b.callCount < 2 {
		t.Fatalf("expected sticky adapter called twice, a=%d b=%d", a.callCount, b.callCount)
	}
}

func TestSessionLoadBalance_AutoPreferredDoesNotStealNewSessions(t *testing.T) {
	guard.GlobalAffinity().Unpin("n1")
	guard.GlobalAffinity().Unpin("n2")

	codex := &mockGroupAdapter{id: "codex:01", priority: 1, group: GroupCodexSub}
	web := &mockGroupAdapter{id: "claude:web:01", priority: 2, group: GroupClaudeWeb}
	pool := NewAccountPoolRouter([]types.ProviderAdapter{codex, web})

	// Simulate leftover preferred from prior auto-switch WITHOUT manual pin.
	pool.mu.Lock()
	pool.preferred = "codex:01"
	pool.manualPin = false
	pool.mu.Unlock()

	ids := make([]string, 0, 2)
	for _, sid := range []string{"n1", "n2"} {
		ch, err := pool.Send(context.Background(), &types.ChatRequest{
			SessionID:     sid,
			ClientDialect: "claude",
			Messages:      []types.ChatMessage{{Role: "user", Content: sid}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, (<-ch).ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("auto preferred stole both sessions: %v (want RR across living)", ids)
	}
}

func TestSessionLoadBalance_ManualPinSticky(t *testing.T) {
	guard.GlobalAffinity().Unpin("m1")
	guard.GlobalAffinity().Unpin("m2")

	codex := &mockGroupAdapter{id: "codex:01", priority: 1, group: GroupCodexSub}
	web := &mockGroupAdapter{id: "claude:web:01", priority: 2, group: GroupClaudeWeb}
	pool := NewAccountPoolRouter([]types.ProviderAdapter{codex, web})
	pool.SetPreferred("claude:web:01")
	if !pool.ManualPin() {
		t.Fatal("SetPreferred must set manualPin")
	}

	for _, sid := range []string{"m1", "m2"} {
		ch, err := pool.Send(context.Background(), &types.ChatRequest{
			SessionID: sid,
			Messages:  []types.ChatMessage{{Role: "user", Content: sid}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if id := (<-ch).ID; id != "claude:web:01" {
			t.Fatalf("manual pin ignored for %s: got %s", sid, id)
		}
	}
}

func TestSessionLoadBalance_ConcurrentSessionsSpread(t *testing.T) {
	adapters := []types.ProviderAdapter{
		&mockGroupAdapter{id: "codex:01", priority: 1, group: GroupCodexSub},
		&mockGroupAdapter{id: "agy:01", priority: 2, group: GroupAGYSub},
		&mockGroupAdapter{id: "chatgpt:01", priority: 3, group: GroupChatGPTWeb},
		&mockGroupAdapter{id: "gemini:web:01", priority: 4, group: GroupGeminiWeb},
		&mockGroupAdapter{id: "claude:web:01", priority: 5, group: GroupClaudeWeb},
	}
	pool := NewAccountPoolRouter(adapters)

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := "c-" + string(rune('a'+i))
			ch, err := pool.Send(context.Background(), &types.ChatRequest{
				SessionID:     sid,
				ClientDialect: "claude",
				Messages:      []types.ChatMessage{{Role: "user", Content: sid}},
			})
			if err != nil {
				t.Errorf("sess %s: %v", sid, err)
				return
			}
			id := (<-ch).ID
			mu.Lock()
			seen[id] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(seen) < 3 {
		t.Fatalf("concurrent sessions should spread across adapters, got %v", seen)
	}
}
