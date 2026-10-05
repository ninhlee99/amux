package router_test

import (
	"context"
	"strings"
	"testing"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func drain(t *testing.T, ch <-chan types.StreamChunk) string {
	t.Helper()
	var sb strings.Builder
	for c := range ch {
		sb.WriteString(c.Content)
	}
	return sb.String()
}

func familyPool(adapters ...types.ProviderAdapter) *router.AccountPoolRouter {
	r := router.NewAccountPoolRouter(adapters)
	r.SetDirectory(adapters)
	return r
}

func hi() *types.ChatRequest {
	return &types.ChatRequest{Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}}
}

func TestSendProvider_FamilyPicksWithinFamilyAndFailsOver(t *testing.T) {
	gw1 := &mockAdapter{id: "gemini:web:01", priority: 22, err: types.ErrRateLimitReached}
	gw2 := &mockAdapter{id: "gemini:web:02", priority: 23, content: "from gw2"}
	api := &mockAdapter{id: "groq:api:01", priority: 1, content: "from groq"}
	r := familyPool(api, gw1, gw2)

	ch, err := r.SendProvider(context.Background(), "gemini:web", hi())
	if err != nil {
		t.Fatalf("SendProvider: %v", err)
	}
	if got := drain(t, ch); got != "from gw2" {
		t.Fatalf("got %q — must fail over inside the family, never to groq", got)
	}
}

func TestSendProvider_WildcardAndCaseInsensitive(t *testing.T) {
	r := familyPool(&mockAdapter{id: "chatgpt:01", priority: 20, content: "gpt"}, &mockAdapter{id: "chatgpt:ninhle", priority: 21, content: "gpt2"})
	for _, target := range []string{"chatgpt", "ChatGPT", "chatgpt:*", "chatgpt:"} {
		ch, err := r.SendProvider(context.Background(), target, hi())
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if got := drain(t, ch); got != "gpt" && got != "gpt2" {
			t.Fatalf("%s: got %q", target, got)
		}
	}
}

func TestSendProvider_ExactIDPinsWithoutFailover(t *testing.T) {
	r := familyPool(&mockAdapter{id: "gemini:web:01", priority: 22, err: types.ErrAuthentication},
		&mockAdapter{id: "gemini:web:02", priority: 23, content: "other"})
	if _, err := r.SendProvider(context.Background(), "gemini:web:01", hi()); err == nil {
		t.Fatal("an exact id must not fail over to a sibling account")
	}
}

func TestSendProvider_NoMatch(t *testing.T) {
	r := familyPool(&mockAdapter{id: "groq:api:01", priority: 1, content: "x"})
	_, err := r.SendProvider(context.Background(), "gemini:web", hi())
	if err == nil || !strings.Contains(err.Error(), "matches no account") {
		t.Fatalf("err = %v", err)
	}
	// "gemini:w" is not a family boundary of "gemini:web:01".
	r2 := familyPool(&mockAdapter{id: "gemini:web:01", priority: 1, content: "x"})
	if _, err := r2.SendProvider(context.Background(), "gemini:w", hi()); err == nil {
		t.Fatal("partial segment must not match")
	}
}

func TestSendProvider_ManualPinOutsideFamilyIgnored(t *testing.T) {
	gw := &mockAdapter{id: "gemini:web:31", priority: 22, content: "gemini"}
	api := &mockAdapter{id: "groq:api:31", priority: 1, content: "groq"}
	r := familyPool(api, gw)
	r.SetPreferred("groq:api:31")
	ch, err := r.SendProvider(context.Background(), "gemini:web", hi())
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch); got != "gemini" {
		t.Fatalf("got %q", got)
	}
}

func TestSend_SkipsManualOnlyAccounts(t *testing.T) {
	manual := &mockAdapter{id: "groq:api:41", priority: 1, content: "manual"}
	auto := &mockAdapter{id: "kimi:api:41", priority: 2, content: "auto"}
	r := familyPool(manual, auto)
	r.SetAutoRotateFilter(func(id string) bool { return id != "groq:api:41" })

	ch, err := r.Send(context.Background(), hi())
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch); got != "auto" {
		t.Fatalf("manual-only account was auto-selected: %q", got)
	}
	// An explicit pin still reaches it.
	ch, err = r.SendProvider(context.Background(), "groq:api:41", hi())
	if err != nil || drain(t, ch) != "manual" {
		t.Fatalf("explicit pin to manual-only account failed: %v", err)
	}
	// A family pick is automatic, so it skips manual-only members too.
	if _, err := r.SendProvider(context.Background(), "groq:api", hi()); err == nil {
		t.Fatal("family pick must not auto-select a manual-only account")
	}
}

func TestSend_SubscriptionOnlyAfterManualPoolAdd(t *testing.T) {
	sub := &mockAdapter{id: "codex:51", priority: 1, content: "sub"}
	web := &mockAdapter{id: "chatgpt:web:51", priority: 20, content: "web"}
	r := familyPool(sub, web)

	// No pool filter at all: an IDE subscription is never picked on its own.
	ch, err := r.Send(context.Background(), hi())
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch); got != "web" {
		t.Fatalf("subscription auto-selected without being in the pool: %q", got)
	}

	// Out of the pool: still skipped, and no failover onto it either.
	r.SetSubscriptionPoolFilter(func(string) bool { return false })
	web.err = types.ErrRateLimitReached
	if _, err := r.Send(context.Background(), hi()); err == nil {
		t.Fatal("must not fail over onto a subscription that is not in the pool")
	}
	// An explicit pin still reaches it.
	ch, err = r.SendProvider(context.Background(), "codex:51", hi())
	if err != nil || drain(t, ch) != "sub" {
		t.Fatalf("explicit pin to subscription failed: %v", err)
	}

	// Added to the pool by hand: now it takes part in rotation.
	r.SetSubscriptionPoolFilter(func(id string) bool { return id == "codex:51" })
	ch, err = r.Send(context.Background(), hi())
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, ch); got != "sub" {
		t.Fatalf("pooled subscription not used: %q", got)
	}
}
