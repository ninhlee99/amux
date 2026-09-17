package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func TestServeWithResilience_RotateOn429(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	rot := NewRotator("claude")
	rot.order = []string{"profile-a", "profile-b"}
	rot.idx = 0
	rot.tokens = map[string]*types.Token{
		"profile-a": {Access: "tok-a", ExpiresAt: time.Now().Add(time.Hour)},
		"profile-b": {Access: "tok-b", ExpiresAt: time.Now().Add(time.Hour)},
	}

	var callCount int32
	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			// Simulate Anthropic 429 response on profile-a
			rot.Observe(&http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"60"}},
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
			return
		}

		// Second call succeeds on profile-b
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: message_start\ndata: {}\n\n"))
	})

	mode := &ProxyMode{}
	pool := router.NewAccountPoolRouter(nil)
	body := []byte(`{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hello"}]}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	serveWithResilience(rec, req, rp, rot, pool, mode, body, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK after retry, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "message_start") {
		t.Fatalf("expected stream content from second profile, got: %s", rec.Body.String())
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestServeWithResilience_FailoverToPoolOnExhausted429(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	rot := NewRotator("claude")
	rot.order = []string{"profile-a"}
	rot.idx = 0
	rot.tokens = map[string]*types.Token{
		"profile-a": {Access: "tok-a", ExpiresAt: time.Now().Add(time.Hour)},
	}

	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rot.Observe(&http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"60"}},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	})

	mode := &ProxyMode{}
	stub := &stubAdapter{id: "fallback-pool-adapter"}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{stub})
	body := []byte(`{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hello"}],"stream":true}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	serveWithResilience(rec, req, rp, rot, pool, mode, body, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK on pool failover, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: message_start") {
		t.Fatalf("expected stream content from pool adapter, got: %s", rec.Body.String())
	}
	if mode.Get() != "provider" {
		t.Errorf("expected mode to switch to provider, got %s", mode.Get())
	}
}
