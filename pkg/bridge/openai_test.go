package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "am-bridge-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("AM_HOME", dir)
	guard.PaceBaseMs = 1
	guard.PaceJitterMs = 1
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type mockStreamAdapter struct {
	id     string
	group  string
	chunks []types.StreamChunk
}

func (m *mockStreamAdapter) ID() string    { return m.id }
func (m *mockStreamAdapter) Priority() int { return 1 }
func (m *mockStreamAdapter) Group() string { return m.group }
func (m *mockStreamAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestHandleChatCompletions_Streaming(t *testing.T) {
	adapter := &mockStreamAdapter{
		id: "test-adapter",
		chunks: []types.StreamChunk{
			{ID: "test-adapter", Content: "Hello "},
			{ID: "test-adapter", Content: "world!"},
			{ID: "test-adapter", Done: true},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{adapter})

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	bridge.HandleChatCompletions(rec, req, pool)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Hello ") || !strings.Contains(body, "world!") {
		t.Errorf("missing content in stream body: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Errorf("expected finish_reason stop chunk, got: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Errorf("expected stream to end with data: [DONE], got: %s", body)
	}
}

func TestHandleChatCompletions_NonStreaming(t *testing.T) {
	adapter := &mockStreamAdapter{
		id: "test-adapter",
		chunks: []types.StreamChunk{
			{ID: "test-adapter", Content: "Non-streaming "},
			{ID: "test-adapter", Content: "response"},
			{ID: "test-adapter", Done: true},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{adapter})

	reqBody := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	bridge.HandleChatCompletions(rec, req, pool)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Choices) == 0 || !strings.Contains(resp.Choices[0].Message.Content, "Non-streaming") {
		t.Fatalf("unexpected content: %+v", resp)
	}
}

// TestHandleChatCompletions_UAIgnoredForRouting locks in the account-equality
// rule: which IDE/client is asking (via User-Agent) must never bias account
// selection — only tier (subscription > web > api_key) and availability do.
// Both adapters here are the same tier, so the caller's User-Agent must not
// change which one answers first.
func TestHandleChatCompletions_UAIgnoredForRouting(t *testing.T) {
	newPool := func() *router.AccountPoolRouter {
		return router.NewAccountPoolRouter([]types.ProviderAdapter{
			&mockStreamAdapter{
				id:     "claude:ua:01",
				group:  "claude_sub",
				chunks: []types.StreamChunk{{ID: "claude:ua:01", Content: "response from claude:ua:01", Done: true}},
			},
			&mockStreamAdapter{
				id:     "codex:ua:01",
				group:  "codex_sub",
				chunks: []types.StreamChunk{{ID: "codex:ua:01", Content: "response from codex:ua:01", Done: true}},
			},
		})
	}

	reqBody := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hi"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("User-Agent", "codex_cli_rs/0.42.0")
	rec := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec, req, newPool())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	firstPick := rec.Body.String()

	cursor := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	cursor.Header.Set("User-Agent", "Cursor/1.0")
	rec2 := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec2, cursor, newPool())
	secondPick := rec2.Body.String()

	pick := func(body string) string {
		switch {
		case strings.Contains(body, "claude:ua:01"):
			return "claude:ua:01"
		case strings.Contains(body, "codex:ua:01"):
			return "codex:ua:01"
		default:
			return ""
		}
	}
	if pick(firstPick) != pick(secondPick) {
		t.Fatalf("User-Agent must not change tier-equal selection: codex-UA picked %q, cursor-UA picked %q", pick(firstPick), pick(secondPick))
	}
}

type failSendAdapter struct {
	id  string
	err error
}

func (m *failSendAdapter) ID() string    { return m.id }
func (m *failSendAdapter) Priority() int { return 1 }
func (m *failSendAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, m.err
}

func TestHandleChatCompletions_StreamErrorEndsWithDone(t *testing.T) {
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{
		&failSendAdapter{id: "fail:01", err: errors.New("upstream down")},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	rec := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec, req, pool)
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "upstream down") {
		t.Fatalf("missing stream error: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream error omitted [DONE]: %s", body)
	}
}
