package router_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

// asyncLimitAdapter accepts the request, then reports a usage limit inside
// the stream — how Gemini Web and ChatGPT Web signal limits.
type asyncLimitAdapter struct {
	id    string
	calls int
}

func (a *asyncLimitAdapter) ID() string    { return a.id }
func (a *asyncLimitAdapter) Priority() int { return 1 }
func (a *asyncLimitAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	a.calls++
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{ID: a.id, Error: fmt.Errorf("%s: %w: usage limit", a.id, types.ErrRateLimitReached)}
	close(ch)
	return ch, nil
}

func TestSend_FailsOverOnInStreamRateLimit(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)

	limited := &asyncLimitAdapter{id: "gemini:web:a"}
	healthy := &mockAdapter{id: "gemini:web:b", priority: 2, content: "from b"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{limited, healthy})

	for i := 0; i < 3; i++ {
		ch, err := r.Send(context.Background(), &types.ChatRequest{
			Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("send %d: want failover to healthy account, got %v", i, err)
		}
		var text strings.Builder
		for c := range ch {
			if c.Error != nil {
				t.Fatalf("send %d: error leaked to client: %v", i, c.Error)
			}
			text.WriteString(c.Content)
		}
		if text.String() != "from b" {
			t.Fatalf("send %d: got %q, want %q", i, text.String(), "from b")
		}
	}
	if limited.calls != 1 {
		t.Errorf("limited account must cool down after its first limit, called %d times", limited.calls)
	}
}

// A stream that already produced content keeps its mid-stream errors: the
// client has seen output, so the router must not silently restart elsewhere.
func TestSend_KeepsStreamAfterFirstContent(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)

	a := &midErrAdapter{id: "chatgpt:x"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a})
	ch, err := r.Send(context.Background(), &types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got []types.StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 || got[0].Content != "partial" || got[1].Error == nil {
		t.Fatalf("want content then error, got %+v", got)
	}
}

type midErrAdapter struct{ id string }

func (a *midErrAdapter) ID() string    { return a.id }
func (a *midErrAdapter) Priority() int { return 1 }
func (a *midErrAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{ID: a.id, Content: "partial"}
	ch <- types.StreamChunk{ID: a.id, Error: fmt.Errorf("connection reset")}
	close(ch)
	return ch, nil
}

// An automatic switch shows in status but never pins: later sessions still
// spread over the pool.
func TestNoteAutoSwitch_DoesNotPin(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)
	a := &mockAdapter{id: "chatgpt:a", content: "a"}
	b := &mockAdapter{id: "chatgpt:b", content: "b"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a, b})
	r.NoteAutoSwitch("chatgpt:a")
	if r.ManualPin() || r.Preferred() != "chatgpt:a" {
		t.Fatalf("auto switch must show as preferred without a manual pin (pin=%v pref=%q)", r.ManualPin(), r.Preferred())
	}
	served := map[string]bool{}
	for i := 0; i < 4; i++ {
		ch, err := r.Send(context.Background(), &types.ChatRequest{SessionID: fmt.Sprintf("s%d", i), Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatal(err)
		}
		for c := range ch {
			served[c.Content] = true
		}
	}
	if !served["a"] || !served["b"] {
		t.Fatalf("new sessions must be balanced over the pool, served %v", served)
	}
}

// One session stays on one account turn after turn, while new sessions spread.
func TestSend_SessionStaysOnItsAccount(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)
	a := &mockAdapter{id: "web:a", content: "a"}
	b := &mockAdapter{id: "web:b", content: "b"}
	r := router.NewAccountPoolRouter([]types.ProviderAdapter{a, b})
	first := ""
	for i := 0; i < 4; i++ {
		ch, err := r.Send(context.Background(), &types.ChatRequest{SessionID: "one-session", Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for c := range ch {
			got += c.Content
		}
		if first == "" {
			first = got
		} else if got != first {
			t.Fatalf("turn %d moved the session from %s to %s", i, first, got)
		}
	}
}

// slowAdapter answers only after release closes, like a web account that is
// still preparing its first chunk.
type slowAdapter struct {
	id      string
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *slowAdapter) ID() string    { return s.id }
func (s *slowAdapter) Priority() int { return 1 }
func (s *slowAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	ch := make(chan types.StreamChunk, 1)
	go func() {
		<-s.release
		ch <- types.StreamChunk{ID: s.id, Content: s.id, Done: true}
		close(ch)
	}()
	return ch, nil
}

// Concurrent new sessions spread over the pool even before any of them has
// received its first chunk.
func TestSend_ConcurrentNewSessionsSpread(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)
	release := make(chan struct{})
	var adapters []types.ProviderAdapter
	var slows []*slowAdapter
	for _, id := range []string{"web:a", "web:b", "web:c", "web:d"} {
		s := &slowAdapter{id: id, release: release}
		slows = append(slows, s)
		adapters = append(adapters, s)
	}
	r := router.NewAccountPoolRouter(adapters)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, err := r.Send(context.Background(), &types.ChatRequest{SessionID: fmt.Sprintf("c%d", i), Messages: []types.ChatMessage{{Role: "user", Content: "hi"}}})
			if err == nil {
				for range ch {
				}
			}
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	for _, s := range slows {
		if s.calls != 1 {
			t.Fatalf("%s got %d of 4 concurrent sessions, want 1 each", s.id, s.calls)
		}
	}
}
