package bridge_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type unreachableAdapter struct{}

func (unreachableAdapter) ID() string    { return "web-offline" }
func (unreachableAdapter) Priority() int { return 1 }
func (unreachableAdapter) SendMessageStream(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, fmt.Errorf("web-offline: sentinel: HTTP 403 Cloudflare challenge: %w", types.ErrUpstreamUnreachable)
}

// Offline, a streaming Claude Code request used to get 200 + message_start
// and then an error event: "Part of the response never arrived", no retry.
// A fast routing failure must be a real 503 + Retry-After the client retries.
func TestHandleClaudeMessages_StreamingUnreachableIsRetryable503(t *testing.T) {
	guard.ResetAll()
	t.Cleanup(guard.ResetAll)
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{unreachableAdapter{}})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	body := []byte(`{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	for i := 0; i < 2; i++ { // second call hits the router's network cooldown
		rec := httptest.NewRecorder()
		_ = bridge.HandleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)), pool, body)
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("call %d: status %d retry-after %q, want 503 with Retry-After", i, rec.Code, rec.Header().Get("Retry-After"))
		}
		if out := rec.Body.String(); strings.Contains(out, "message_start") || strings.Contains(out, "<html") {
			t.Fatalf("call %d: opened a stream or leaked HTML: %s", i, out)
		}
	}
}
