package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func TestRewriteClaudeModelInBody(t *testing.T) {
	origJSON := `{"model":"claude-opus-5","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":1000}`
	rewritten := rewriteClaudeModelInBody([]byte(origJSON), "claude-sonnet-5")

	var parsed map[string]any
	if err := json.Unmarshal(rewritten, &parsed); err != nil {
		t.Fatalf("failed to unmarshal rewritten body: %v", err)
	}

	if parsed["model"] != "claude-sonnet-5" {
		t.Fatalf("expected model to be claude-sonnet-5, got %v", parsed["model"])
	}

	if parsed["stream"] != true {
		t.Fatalf("expected stream=true preserved, got %v", parsed["stream"])
	}

	if !strings.Contains(string(rewritten), "hello") {
		t.Fatalf("expected messages content preserved, got %s", string(rewritten))
	}
}

func TestRewriteClaudeModelInBody_EmptyModel(t *testing.T) {
	origJSON := `{"messages":[{"role":"user","content":"test"}]}`
	rewritten := rewriteClaudeModelInBody([]byte(origJSON), "claude-sonnet-5")

	var parsed map[string]any
	if err := json.Unmarshal(rewritten, &parsed); err != nil {
		t.Fatalf("failed to unmarshal rewritten body: %v", err)
	}

	if parsed["model"] != "claude-sonnet-5" {
		t.Fatalf("expected model to be claude-sonnet-5, got %v", parsed["model"])
	}
}

func TestHandler_OpusModelRewrittenToSonnet(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_MODEL", "")

	var capturedBody []byte
	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = b
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}`))
	})

	rot := NewRotator("claude")
	rot.order = []string{"test-prof"}
	rot.tokens = map[string]*types.Token{"test-prof": {Access: "dummy-token"}}
	life := NewLifecycle()
	mode := &ProxyMode{}
	pool := router.NewAccountPoolRouter(nil)
	sw := &swappableHandler{}
	h := newHandler(rot, life, mode, pool, pool, rp, "https://api.anthropic.com", sw, "", func() {})
	sw.Set(h)

	reqBody := `{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	sw.ServeHTTP(w, req)

	if len(capturedBody) == 0 {
		t.Fatalf("expected rp to be called, got response %d: %s", w.Code, w.Body.String())
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}

	if parsed["model"] != "claude-sonnet-5" {
		t.Fatalf("expected model in captured request to be rewritten to claude-sonnet-5, got %v", parsed["model"])
	}
}

func TestHandler_ExplicitOpusRespected(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-5")

	var capturedBody []byte
	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = b
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}`))
	})

	rot := NewRotator("claude")
	rot.order = []string{"test-prof"}
	rot.tokens = map[string]*types.Token{"test-prof": {Access: "dummy-token"}}
	life := NewLifecycle()
	mode := &ProxyMode{}
	pool := router.NewAccountPoolRouter(nil)
	sw := &swappableHandler{}
	h := newHandler(rot, life, mode, pool, pool, rp, "https://api.anthropic.com", sw, "", func() {})
	sw.Set(h)

	reqBody := `{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	sw.ServeHTTP(w, req)

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}

	if parsed["model"] != "claude-opus-5" {
		t.Fatalf("expected explicit opus model to be kept, got %v", parsed["model"])
	}
}
