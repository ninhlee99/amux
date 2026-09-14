package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/types"
)

type mockGroupAdapter struct {
	id          string
	priority    int
	group       string
	failWith429 bool
	callCount   int
}

func (m *mockGroupAdapter) ID() string    { return m.id }
func (m *mockGroupAdapter) Priority() int { return m.priority }
func (m *mockGroupAdapter) Group() string { return m.group }

func (m *mockGroupAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	m.callCount++
	if m.failWith429 {
		return nil, types.ErrRateLimitReached
	}
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{Content: fmt.Sprintf("response from %s", m.id), Done: true}
	close(ch)
	return ch, nil
}

func TestDetermineAdapterGroup(t *testing.T) {
	tests := []struct {
		id       string
		group    string
		expected string
	}{
		{id: "claude:web:01", expected: GroupClaudeWeb},
		{id: "chatgpt:01", expected: GroupChatGPTWeb},
		{id: "gemini:web:01", expected: GroupGeminiWeb},
		{id: "codex:01", expected: GroupCodexSub},
		{id: "codex:free:01", expected: GroupCodexFree},
		{id: "agy:01", expected: GroupAGYSub},
		{id: "agy:free:01", expected: GroupAGYFree},
		{id: "gemini:api:01", expected: GroupAPIOther},
		{id: "groq:api:01", expected: GroupAPIOther},
		{id: "openrouter:api:01", expected: GroupAPIOther},
	}

	for _, tc := range tests {
		a := &mockGroupAdapter{id: tc.id, group: tc.group}
		got := DetermineAdapterGroup(a)
		if got != tc.expected {
			t.Errorf("adapter %s: expected group %s, got %s", tc.id, tc.expected, got)
		}
	}
}

func TestAccountPoolRouter_GroupPriorityAndIntraGroupRotation(t *testing.T) {
	// Setup adapters across groups:
	// Group 2: codex_sub (codex:01, codex:02)
	// Group 3: agy_sub (agy:01)
	// Group 7: api_other (gemini:api:01)
	codex1 := &mockGroupAdapter{id: "codex:01", priority: 1, group: GroupCodexSub}
	codex2 := &mockGroupAdapter{id: "codex:02", priority: 2, group: GroupCodexSub}
	agy1 := &mockGroupAdapter{id: "agy:01", priority: 5, group: GroupAGYSub}
	gemini1 := &mockGroupAdapter{id: "gemini:api:01", priority: 10, group: GroupAPIOther}

	adapters := []types.ProviderAdapter{gemini1, codex2, agy1, codex1}
	r := NewAccountPoolRouter(adapters)

	ctx := context.Background()
	req := &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "hello"}}}

	// Turn 1: Should pick codex:01 (highest group: codex_sub)
	ch, err := r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}
	chunk := <-ch
	if chunk.Content != "response from codex:01" {
		t.Fatalf("Turn 1 expected codex:01, got: %s", chunk.Content)
	}

	// Turn 2: Should rotate within codex_sub to codex:02
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from codex:02" {
		t.Fatalf("Turn 2 expected codex:02, got: %s", chunk.Content)
	}

	// Turn 3: Should rotate within codex_sub back to codex:01
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 3 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from codex:01" {
		t.Fatalf("Turn 3 expected codex:01, got: %s", chunk.Content)
	}

	// Now codex:01 hits 429
	codex1.failWith429 = true

	// Turn 4: Should fail over to codex:02 (still in codex_sub!)
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 4 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from codex:02" {
		t.Fatalf("Turn 4 expected codex:02, got: %s", chunk.Content)
	}

	// Now codex:02 also hits 429 -> all codex_sub exhausted!
	codex2.failWith429 = true

	// Turn 5: Should fail over to Group 3 (agy_sub -> agy:01)
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 5 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from agy:01" {
		t.Fatalf("Turn 5 expected agy:01, got: %s", chunk.Content)
	}

	// Now agy:01 also hits 429 -> agy_sub exhausted!
	agy1.failWith429 = true

	// Turn 6: Should fail over to Group 7 (api_other -> gemini:api:01)
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 6 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from gemini:api:01" {
		t.Fatalf("Turn 6 expected gemini:api:01, got: %s", chunk.Content)
	}

	// Verify cooldown recovery:
	// Clear cooldown for codex:01 by resetting router cooldown map and guard pacer/health
	r.mu.Lock()
	r.cooldownMap["codex:01"] = time.Now().Add(-1 * time.Minute)
	codex1.failWith429 = false
	r.mu.Unlock()
	guard.GlobalPacer().Reset("codex:01")
	guard.GlobalHealth().Reset("codex:01")

	// Turn 7: Should automatically return to codex:01 (highest available priority group!)
	ch, err = r.Send(ctx, req)
	if err != nil {
		t.Fatalf("Turn 7 failed: %v", err)
	}
	chunk = <-ch
	if chunk.Content != "response from codex:01" {
		t.Fatalf("Turn 7 expected return to codex:01, got: %s", chunk.Content)
	}
}
