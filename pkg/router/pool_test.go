package router_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type mockAdapter struct {
	id       string
	priority int
	err      error
	content  string
}

func (m *mockAdapter) ID() string    { return m.id }
func (m *mockAdapter) Priority() int { return m.priority }
func (m *mockAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{ID: m.id, Content: m.content}
	ch <- types.StreamChunk{ID: m.id, Done: true}
	close(ch)
	return ch, nil
}

func TestAccountPoolRouter_Failover(t *testing.T) {
	// Adapter 1: priority 1, fails with 429
	a1 := &mockAdapter{id: "provider-1", priority: 1, err: types.ErrRateLimitReached}
	// Adapter 2: priority 2, succeeds
	a2 := &mockAdapter{id: "provider-2", priority: 2, content: "hello from provider 2"}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a2, a1}) // unordered
	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	var text string
	for chunk := range ch {
		text += chunk.Content
	}

	if text != "hello from provider 2" {
		t.Fatalf("expected 'hello from provider 2', got %q", text)
	}

	// Now provider-1 should be in cooldown, so next Send goes directly to provider-2
	status := r.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 providers in status, got %d", len(status))
	}
	if !status[0]["cooling"].(bool) {
		t.Fatalf("expected provider-1 to be cooling")
	}
}

// TestAccountPoolRouter_MultiSessionFailover locks in the multi-session
// scenario Phase 3/4 unlock: two accounts of the *same* provider type
// (unified IDs sharing a prefix, e.g. from a second `am login claude`)
// failing over to each other exactly like two different provider types do —
// the router itself needs no ID-format awareness at all.
func TestAccountPoolRouter_MultiSessionFailover(t *testing.T) {
	session1 := &mockAdapter{id: "claudeweb:01", priority: 1, err: types.ErrRateLimitReached}
	session2 := &mockAdapter{id: "claudeweb:02", priority: 2, content: "hello from session 2"}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{session1, session2})
	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	var text string
	for chunk := range ch {
		text += chunk.Content
	}
	if text != "hello from session 2" {
		t.Fatalf("expected failover to claudeweb:02, got %q", text)
	}

	status := r.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 providers in status, got %d", len(status))
	}
	if !status[0]["cooling"].(bool) {
		t.Fatalf("expected claudeweb:01 to be cooling after rate limit")
	}
}

// TestAccountPoolRouter_ToolsDoNotAffectRouting locks in the account-equality
// rule: an adapter must be picked purely by tier/availability, never by
// whether the request carries tools[].
func TestAccountPoolRouter_ToolsDoNotAffectRouting(t *testing.T) {
	web := &mockAdapter{id: "chatgpt:web:01", priority: 1, content: "web-served"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{web})

	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "implement a function"}},
		Tools:    []types.ToolDef{{Name: "Bash"}},
	})
	if err != nil {
		t.Fatalf("web-only pool must still serve tool-bearing requests, got %v", err)
	}
	var text string
	for chunk := range ch {
		text += chunk.Content
	}
	if text != "web-served" {
		t.Fatalf("want web-served regardless of tools[], got %q", text)
	}
}

func TestAccountPoolRouter_SendNamedIgnoresTools(t *testing.T) {
	web := &mockAdapter{id: "chatgpt:web:01", priority: 1, content: "pinned"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{web})
	ch, err := r.SendNamed(context.Background(), "chatgpt:web:01", &types.ChatRequest{
		Tools: []types.ToolDef{{Name: "Bash"}},
	})
	if err != nil {
		t.Fatalf("X-Provider pin must still reach the backend regardless of tools[], got %v", err)
	}
	var text string
	for chunk := range ch {
		text += chunk.Content
	}
	if text != "pinned" {
		t.Fatalf("want pinned backend, got %q", text)
	}
}

func TestAccountPoolRouter_AllFail(t *testing.T) {
	a1 := &mockAdapter{id: "p1", priority: 1, err: errors.New("err1")}
	a2 := &mockAdapter{id: "p2", priority: 2, err: errors.New("err2")}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a1, a2})
	_, err := r.Send(context.Background(), &types.ChatRequest{})
	if err == nil {
		t.Fatalf("expected error when all fail")
	}
}

func TestAccountPoolRouter_ConcurrentRace(t *testing.T) {
	a1 := &mockAdapter{id: "p1", priority: 1, err: types.ErrRateLimitReached}
	a2 := &mockAdapter{id: "p2", priority: 2, content: "p2"}
	a3 := &mockAdapter{id: "p3", priority: 3, content: "p3"}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a1, a2, a3})

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()

			// Concurrently read status
			_ = r.Status()

			// Concurrently read preferred
			_ = r.Preferred()

			// Occasionally update preferred
			if idx%5 == 0 {
				r.SetPreferred("p3")
			}

			// Concurrently send requests
			ch, err := r.Send(ctx, &types.ChatRequest{Model: "test"})
			if err == nil {
				for range ch {
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestAccountPoolRouter_AdaptiveCooldownRetryAfter(t *testing.T) {
	// a1 returns RateLimitError with a 10-second RetryAfter hint
	hintDuration := 10 * time.Second
	a1 := &mockAdapter{
		id:       "adapter-retry-after",
		priority: 1,
		err:      types.NewRateLimitError("too many requests", hintDuration),
	}
	a2 := &mockAdapter{
		id:       "adapter-backup",
		priority: 2,
		content:  "backup content",
	}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a1, a2})

	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("expected successful failover to backup, got %v", err)
	}
	for range ch {
	}

	// Verify a1 status reports cooling
	status := r.Status()
	var a1Status map[string]any
	for _, s := range status {
		if s["id"] == "adapter-retry-after" {
			a1Status = s
			break
		}
	}
	if a1Status == nil {
		t.Fatalf("adapter-retry-after not found in status")
	}
	if a1Status["cooling"] != true {
		t.Fatalf("expected adapter-retry-after to be in cooldown")
	}
	cdStr, ok := a1Status["cooldown_until"].(string)
	if !ok || cdStr == "" {
		t.Fatalf("expected valid cooldown_until string, got %v", a1Status["cooldown_until"])
	}
}

type capturingAdapter struct {
	mockAdapter
	lastReq *types.ChatRequest
}

func (c *capturingAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	c.lastReq = req
	return c.mockAdapter.SendMessageStream(ctx, req)
}

func TestAccountPoolRouter_FailoverCompactsContextForColdAccount(t *testing.T) {
	a1 := &mockAdapter{
		id:       "adapter-failing",
		priority: 1,
		err:      types.ErrRateLimitReached,
	}
	a2 := &capturingAdapter{
		mockAdapter: mockAdapter{
			id:       "adapter-cold-backup",
			priority: 2,
			content:  "backup content",
		},
	}

	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a1, a2})

	// Build a long conversation history (15 turns)
	longMsgs := []types.ChatMessage{
		{Role: "system", Content: "You are an expert assistant."},
		{Role: "user", Content: "Root goal: optimize database queries"},
	}
	for i := 1; i <= 12; i++ {
		longMsgs = append(longMsgs, types.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("Intermediate query %d with long log data...", i),
		})
	}
	longMsgs = append(longMsgs, types.ChatMessage{
		Role:    "user",
		Content: "Final user turn: please run benchmark now",
	})

	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: longMsgs,
	})
	if err != nil {
		t.Fatalf("expected failover to succeed, got %v", err)
	}
	for range ch {
	}

	if a2.lastReq == nil {
		t.Fatalf("adapter 2 was never called")
	}

	// Verify that adapter 2 received compacted messages, not the full 15 messages!
	if len(a2.lastReq.Messages) >= len(longMsgs) {
		t.Fatalf("expected messages to be compacted on failover, got %d messages (orig %d)",
			len(a2.lastReq.Messages), len(longMsgs))
	}
	// Verify system and root prompt are preserved
	if a2.lastReq.Messages[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %s", a2.lastReq.Messages[0].Role)
	}
	if a2.lastReq.Messages[1].Content != "Root goal: optimize database queries" {
		t.Fatalf("expected root goal preserved, got %q", a2.lastReq.Messages[1].Content)
	}
}

type mockGroupAdapter struct {
	mockAdapter
	grp string
}

func (m *mockGroupAdapter) Group() string {
	return m.grp
}

// TestAccountPoolRouter_TierStrictPriority locks in the cost/quota-only
// priority order: subscription > web > api_key, regardless of task kind or
// which adapter was registered first.
func TestAccountPoolRouter_TierStrictPriority(t *testing.T) {
	aSub := &mockGroupAdapter{mockAdapter: mockAdapter{id: "codex-sub-1", priority: 1, content: "sub"}, grp: "codex_sub"}
	aWeb := &mockGroupAdapter{mockAdapter: mockAdapter{id: "claude-web-1", priority: 1, content: "web"}, grp: "claude_web"}
	aAPI := &mockGroupAdapter{mockAdapter: mockAdapter{id: "openrouter-api", priority: 1, content: "api"}, grp: "api_other"}

	adapters := []types.ProviderAdapter{aWeb, aAPI, aSub}
	r := router.NewAccountPoolRouter(adapters)

	send := func() string {
		ch, err := r.Send(context.Background(), &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		var text string
		for chunk := range ch {
			text += chunk.Content
		}
		return text
	}

	// 1. Subscription tier wins first, regardless of task kind.
	if got := send(); got != "sub" {
		t.Fatalf("expected subscription tier first, got %q", got)
	}

	// 2. Subscription cooled down: web tier next.
	r.SetCooldownForTest("codex-sub-1", 10*time.Minute)
	if got := send(); got != "web" {
		t.Fatalf("expected web tier next, got %q", got)
	}

	// 3. Web cooled down too: API key last resort.
	r.SetCooldownForTest("claude-web-1", 10*time.Minute)
}

func TestRouter_AutoRotateFilterExclusion(t *testing.T) {
	subActive := &mockGroupAdapter{mockAdapter: mockAdapter{id: "claude-sub-active", priority: 1, content: "sub-active"}, grp: "claude_sub"}
	subVIP := &mockGroupAdapter{mockAdapter: mockAdapter{id: "claude-sub-vip", priority: 2, content: "sub-vip"}, grp: "claude_sub"}
	webFallback := &mockGroupAdapter{mockAdapter: mockAdapter{id: "claude-web-fallback", priority: 1, content: "web-fallback"}, grp: "claude_web"}
	webManual := &mockGroupAdapter{mockAdapter: mockAdapter{id: "chatgpt-web-manual", priority: 2, content: "web-manual"}, grp: "chatgpt_web"}

	adapters := []types.ProviderAdapter{subActive, subVIP, webFallback, webManual}
	r := router.NewAccountPoolRouter(adapters)

	// Configure filter: sub-vip and web-manual are AUTO-SWITCH = OFF (manual only)
	autoSwitchState := map[string]bool{
		"claude-sub-active":   true,
		"claude-sub-vip":      false, // OFF
		"claude-web-fallback": true,
		"chatgpt-web-manual":  false, // OFF
	}
	r.SetAutoRotateFilter(func(id string) bool {
		return autoSwitchState[id]
	})

	send := func() string {
		ch, err := r.Send(context.Background(), &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		var text string
		for chunk := range ch {
			text += chunk.Content
		}
		return text
	}

	// 1. Initial request: auto-route selects claude-sub-active
	if got := send(); got != "sub-active" {
		t.Fatalf("expected claude-sub-active, got %q", got)
	}

	// 2. claude-sub-active cools down.
	// Auto-switch must NOT pick claude-sub-vip (it's OFF)!
	// It should skip to next tier (Web) and pick claude-web-fallback (ON), skipping chatgpt-web-manual (OFF)!
	r.SetCooldownForTest("claude-sub-active", 10*time.Minute)
	if got := send(); got != "web-fallback" {
		t.Fatalf("expected auto-switch to web-fallback (skipping sub-vip and web-manual), got %q", got)
	}

	// 3. Manual pin to claude-sub-vip (via amux id select / SetPreferred)
	r.SetPreferred("claude-sub-vip")
	if got := send(); got != "sub-vip" {
		t.Fatalf("expected manual pin to claude-sub-vip to work, got %q", got)
	}

	// 4. Clear preferred pin: auto-switch resumes, should still skip sub-vip and choose web-fallback
	r.ClearPreferred()
	if got := send(); got != "web-fallback" {
		t.Fatalf("expected web-fallback after clearing pin, got %q", got)
	}

	// 5. Cooldown web-fallback too: no living auto-rotatable accounts left
	r.SetCooldownForTest("claude-web-fallback", 10*time.Minute)
	if r.HasLivingAccounts() {
		t.Fatalf("expected HasLivingAccounts to be false when all ON accounts are cooling down")
	}
}


