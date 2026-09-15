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
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "am-bridge-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("AM_HOME", dir)
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

func TestHandleChatCompletions_CodexUAPrefersCodexSub(t *testing.T) {
	claude := &mockStreamAdapter{
		id:     "claude:ua:01",
		group:  "claude_sub",
		chunks: []types.StreamChunk{{ID: "claude:ua:01", Content: "response from claude:ua:01", Done: true}},
	}
	codex := &mockStreamAdapter{
		id:     "codex:ua:01",
		group:  "codex_sub",
		chunks: []types.StreamChunk{{ID: "codex:ua:01", Content: "response from codex:ua:01", Done: true}},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{claude, codex})

	reqBody := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("User-Agent", "codex_cli_rs/0.42.0")
	rec := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec, req, pool)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "response from codex:ua:01") {
		t.Fatalf("Codex UA should pick codex_sub first, got %s", rec.Body.String())
	}

	cursorPool := router.NewAccountPoolRouter([]types.ProviderAdapter{
		&mockStreamAdapter{
			id:     "claude:ua:02",
			group:  "claude_sub",
			chunks: []types.StreamChunk{{ID: "claude:ua:02", Content: "response from claude:ua:02", Done: true}},
		},
		&mockStreamAdapter{
			id:     "codex:ua:02",
			group:  "codex_sub",
			chunks: []types.StreamChunk{{ID: "codex:ua:02", Content: "response from codex:ua:02", Done: true}},
		},
	})
	cursor := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	cursor.Header.Set("User-Agent", "Cursor/1.0")
	rec2 := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec2, cursor, cursorPool)
	if !strings.Contains(rec2.Body.String(), "response from claude:ua:02") {
		t.Fatalf("Cursor UA should keep global Claude-first order, got %s", rec2.Body.String())
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
