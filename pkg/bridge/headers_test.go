package bridge

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

// resetPacingForTest clears guard's anti-ban pacer state so tests reusing the
// same mock account ID (e.g. "slow:01") don't inherit spacing reserved by a
// previous test/subtest, which previously made requests wait 10-15s or made
// worker entry checks (which time out after 1s) fail spuriously.
func resetPacingForTest(t *testing.T) {
	t.Helper()
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)
}

func TestBeginSSE_CommitsKeepaliveBeforeProviderWork(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher, ok := beginSSE(rec)
	if !ok || flusher == nil {
		t.Fatal("expected recorder SSE flusher")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), ": amux stream connected") {
		t.Fatalf("missing initial SSE keepalive: %q", rec.Body.String())
	}
}

type delayAdapter struct {
	id      string
	delay   time.Duration
	entered chan struct{}
	block   <-chan struct{}
	done    atomic.Bool
}

func (d *delayAdapter) ID() string    { return d.id }
func (d *delayAdapter) Priority() int { return 1 }
func (d *delayAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	defer d.done.Store(true)
	if d.entered != nil {
		select {
		case d.entered <- struct{}{}:
		default:
		}
	}
	if d.block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-d.block:
		}
	}
	if d.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d.delay):
		}
	}
	ch := make(chan types.StreamChunk, 1)
	ch <- types.StreamChunk{Content: "ok", Done: true}
	close(ch)
	return ch, nil
}

func TestPoolSendStreaming_WritesKeepaliveWhileUpstreamPending(t *testing.T) {
	resetPacingForTest(t)
	old := streamKeepaliveInterval
	streamKeepaliveInterval = 15 * time.Millisecond
	defer func() { streamKeepaliveInterval = old }()

	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{
		&delayAdapter{id: "slow:01", delay: 50 * time.Millisecond},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	flusher, ok := beginSSE(rec)
	if !ok {
		t.Fatal("expected flusher")
	}
	stream, err := poolSendStreaming(rec, req, pool, &types.ChatRequest{}, flusher, nil)
	if err != nil {
		t.Fatalf("poolSendStreaming: %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}
	drainStream(stream)
	if !strings.Contains(rec.Body.String(), ": amux upstream pending") {
		t.Fatalf("missing pending keepalive: %q", rec.Body.String())
	}
}

func TestPoolSendStreaming_CancelWaitsForWorker(t *testing.T) {
	old := streamKeepaliveInterval
	streamKeepaliveInterval = 20 * time.Millisecond
	defer func() { streamKeepaliveInterval = old }()

	entered := make(chan struct{}, 1)
	adapter := &delayAdapter{id: "slow:01", entered: entered, block: make(chan struct{})}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{adapter})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	flusher, ok := beginSSE(rec)
	if !ok {
		t.Fatal("expected flusher")
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := poolSendStreaming(rec, req, pool, &types.ChatRequest{}, flusher, nil)
		errCh <- err
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker never entered SendMessageStream")
	}
	cancel()

	select {
	case err := <-errCh:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("poolSendStreaming did not return after cancel")
	}
	if !adapter.done.Load() {
		t.Fatal("worker still running after handler returned")
	}
}

func TestEnrichRequestMetadata_ProjectExtraction(t *testing.T) {
	httpReq := httptest.NewRequest("POST", "/v1/messages", nil)
	httpReq.Header.Set("X-Project-Root", "/Users/ninh.le/Documents/apps/amux")

	chatReq := &types.ChatRequest{SessionID: "sess-abc"}
	EnrichRequestMetadata(httpReq, chatReq)

	if chatReq.Project() != "/Users/ninh.le/Documents/apps/amux" {
		t.Fatalf("expected project /Users/ninh.le/Documents/apps/amux, got %q", chatReq.Project())
	}
	if chatReq.ScopeKey() != "/Users/ninh.le/Documents/apps/amux::sess-abc" {
		t.Fatalf("expected scope key /Users/ninh.le/Documents/apps/amux::sess-abc, got %q", chatReq.ScopeKey())
	}
}
