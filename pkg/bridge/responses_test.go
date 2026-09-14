package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type mockResponsesBackend struct {
	id     string
	chunks []types.StreamChunk
	req    *types.ChatRequest
}

func (m *mockResponsesBackend) ID() string    { return m.id }
func (m *mockResponsesBackend) Priority() int { return 1 }
func (m *mockResponsesBackend) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	m.req = req
	ch := make(chan types.StreamChunk, len(m.chunks)+1)
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestHandleOpenAIResponses_Streaming(t *testing.T) {
	backend := &mockResponsesBackend{
		id: "chatgpt:01",
		chunks: []types.StreamChunk{
			{ID: "chatgpt:01", Content: "Hello, "},
			{ID: "chatgpt:01", Content: "I am Codex!"},
			{ID: "chatgpt:01", Done: true, FinishReason: "stop"},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	reqBody := map[string]any{
		"model":  "gpt-5-codex",
		"input":  "Say hello",
		"stream": true,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: response.created") {
		t.Errorf("missing response.created event: %s", body)
	}
	if !strings.Contains(body, "response.output_text.delta") {
		t.Errorf("missing response.output_text.delta event: %s", body)
	}
	if !strings.Contains(body, "Hello, ") || !strings.Contains(body, "I am Codex!") {
		t.Errorf("missing expected streamed deltas: %s", body)
	}
	if !strings.Contains(body, "event: response.completed") {
		t.Errorf("missing response.completed event: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("missing [DONE] marker: %s", body)
	}

	// Verify request converted properly
	if backend.req == nil {
		t.Fatalf("backend did not receive ChatRequest")
	}
	if backend.req.Model != "gpt-5-codex" {
		t.Errorf("expected model gpt-5-codex, got %s", backend.req.Model)
	}
	if len(backend.req.Messages) != 1 || backend.req.Messages[0].Content != "Say hello" {
		t.Errorf("unexpected messages: %+v", backend.req.Messages)
	}
}

func TestHandleOpenAIResponses_StreamErrorEndsWithDone(t *testing.T) {
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{
		&failSendAdapter{id: "fail:01", err: errors.New("upstream down")},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(
		`{"model":"gpt-5-codex","input":"hello","stream":true}`,
	))
	rec := httptest.NewRecorder()
	bridge.HandleOpenAIResponses(rec, req, pool)
	body := rec.Body.String()
	if !strings.Contains(body, "response.failed") {
		t.Fatalf("missing response.failed: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream error omitted [DONE]: %s", body)
	}
}

func TestHandleOpenAIResponses_NonStreaming(t *testing.T) {
	backend := &mockResponsesBackend{
		id: "chatgpt:01",
		chunks: []types.StreamChunk{
			{ID: "chatgpt:01", Content: "Non-streaming answer."},
			{ID: "chatgpt:01", Done: true, FinishReason: "stop"},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	reqBody := map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{
			{"role": "user", "content": "Tell me something"},
		},
		"stream": false,
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode json response: %v, raw: %s", err, w.Body.String())
	}

	if resp.Object != "response" || resp.Status != "completed" {
		t.Errorf("unexpected object/status: %+v", resp)
	}
	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 1 || resp.Output[0].Content[0].Text != "Non-streaming answer." {
		t.Errorf("unexpected output: %+v", resp.Output)
	}
	if resp.Usage["total_tokens"] <= 0 {
		t.Errorf("expected non-zero usage: %+v", resp.Usage)
	}
}

func TestHandleOpenAIResponses_ToolCalls(t *testing.T) {
	backend := &mockResponsesBackend{
		id: "chatgpt:01",
		chunks: []types.StreamChunk{
			{
				ID: "chatgpt:01",
				ToolCalls: []types.ToolCall{
					{
						ID:        "call_exec_1",
						Name:      "exec_command",
						Arguments: `{"cmd":"git status"}`,
					},
				},
				Done:         true,
				FinishReason: "tool_calls",
			},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	reqBody := map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"tools": []map[string]any{
			{
				"type":        "function",
				"name":        "exec_command",
				"description": "Run shell command",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cmd": map[string]any{"type": "string"},
					},
				},
			},
		},
		"input": []map[string]any{
			{"role": "user", "content": "Check git status"},
		},
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, `"function_call"`) {
		t.Errorf("missing function_call output item in SSE: %s", body)
	}
	if !strings.Contains(body, `"exec_command"`) {
		t.Errorf("missing exec_command in SSE: %s", body)
	}
	if !strings.Contains(body, `git status`) {
		t.Errorf("missing arguments in SSE: %s", body)
	}

	// Turn 2: Client sends function_call_output
	turn2Backend := &mockResponsesBackend{
		id: "chatgpt:01",
		chunks: []types.StreamChunk{
			{ID: "chatgpt:01", Content: "Working directory is clean."},
			{ID: "chatgpt:01", Done: true, FinishReason: "stop"},
		},
	}
	turn2Pool := router.NewAccountPoolRouter([]types.ProviderAdapter{turn2Backend})

	reqBody2 := map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"input": []map[string]any{
			{"role": "user", "content": "Check git status"},
			{
				"type":      "function_call",
				"call_id":   "call_exec_1",
				"name":      "exec_command",
				"arguments": `{"cmd":"git status"}`,
			},
			{
				"type":    "function_call_output",
				"call_id": "call_exec_1",
				"output":  "On branch feat/provider-kimi-grok\nnothing to commit",
			},
		},
	}
	b2, _ := json.Marshal(reqBody2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w2, req2, turn2Pool)
	if w2.Code != http.StatusOK {
		t.Fatalf("turn 2 expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}

	if len(turn2Backend.req.Messages) < 3 {
		t.Fatalf("expected at least 3 messages in turn 2, got %d: %+v", len(turn2Backend.req.Messages), turn2Backend.req.Messages)
	}
	toolMsg := turn2Backend.req.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_exec_1" || !strings.Contains(toolMsg.Content, "nothing to commit") {
		t.Errorf("unexpected tool message: %+v", toolMsg)
	}
}
