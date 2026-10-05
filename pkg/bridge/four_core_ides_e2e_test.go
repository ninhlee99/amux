package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type mockE2EBackend struct {
	id     string
	onSend func(req *types.ChatRequest) []types.StreamChunk
}

func (m *mockE2EBackend) ID() string             { return m.id }
func (m *mockE2EBackend) Priority() int          { return 1 }
func (m *mockE2EBackend) Group() string          { return "api_other" }
func (m *mockE2EBackend) SupportsTools() bool    { return true }
func (m *mockE2EBackend) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	chunks := m.onSend(req)
	ch := make(chan types.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// 1. E2E Multi-turn test for Claude Code (/v1/messages)
func TestE2E_ClaudeCode_MultiTurn(t *testing.T) {
	turn := 1
	backend := &mockE2EBackend{
		id: "claude_backend",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			if turn == 1 {
				return []types.StreamChunk{
					{
						ID: "turn1",
						ToolCalls: []types.ToolCall{
							{ID: "call_bash_1", Name: "Bash", Arguments: `{"command":"ls -la"}`},
						},
						FinishReason: "tool_calls",
						Done:         true,
					},
				}
			}
			return []types.StreamChunk{
				{ID: "turn2", Content: "Found 3 files in directory.", Done: true},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	// Turn 1: Client prompts with Tools
	body1, _ := json.Marshal(map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": []map[string]any{
			{"name": "Bash", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "List files"},
		},
	})
	req1 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w1, req1, pool, body1); err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}
	resp1 := w1.Body.String()
	if !strings.Contains(resp1, "tool_use") || !strings.Contains(resp1, "Bash") {
		t.Fatalf("Turn 1 expected tool_use event, got: %s", resp1)
	}

	// Turn 2: Client sends tool_result
	turn = 2
	body2, _ := json.Marshal(map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": []map[string]any{
			{"name": "Bash", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "List files"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "call_bash_1", "name": "Bash", "input": map[string]any{"command": "ls -la"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "call_bash_1", "content": "file1.txt\nfile2.txt\nfile3.txt"},
			}},
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w2, req2, pool, body2); err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}
	resp2 := w2.Body.String()
	if !strings.Contains(resp2, "Found 3 files") {
		t.Fatalf("Turn 2 expected completion text, got: %s", resp2)
	}
}

// 2. E2E Multi-turn test for Cursor (/v1/chat/completions)
func TestE2E_Cursor_MultiTurn(t *testing.T) {
	turn := 1
	backend := &mockE2EBackend{
		id: "cursor_backend",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			if turn == 1 {
				return []types.StreamChunk{
					{
						ID: "chatcmpl_1",
						ToolCalls: []types.ToolCall{
							{ID: "call_read_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
						},
						FinishReason: "tool_calls",
						Done:         true,
					},
				}
			}
			return []types.StreamChunk{
				{ID: "chatcmpl_2", Content: "main.go defines the main package.", Done: true},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	// Turn 1
	body1, _ := json.Marshal(map[string]any{
		"model":  "gpt-4o",
		"stream": true,
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name": "read_file",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"path": map[string]any{"type": "string"}},
					},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Read main.go"},
		},
	})
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	bridge.HandleChatCompletions(w1, req1, pool)
	resp1 := w1.Body.String()
	if !strings.Contains(resp1, "tool_calls") || !strings.Contains(resp1, "read_file") {
		t.Fatalf("Cursor Turn 1 expected tool_calls, got: %s", resp1)
	}

	// Turn 2
	turn = 2
	body2, _ := json.Marshal(map[string]any{
		"model":  "gpt-4o",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "Read main.go"},
			{
				"role": "assistant",
				"tool_calls": []map[string]any{
					{"id": "call_read_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"main.go"}`}},
				},
			},
			{"role": "tool", "tool_call_id": "call_read_1", "content": "package main\nfunc main() {}"},
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	bridge.HandleChatCompletions(w2, req2, pool)
	resp2 := w2.Body.String()
	if !strings.Contains(resp2, "main.go defines the main package") {
		t.Fatalf("Cursor Turn 2 expected completion, got: %s", resp2)
	}
}

// 3. E2E Multi-turn test for Google Antigravity AGY (/v1beta Gemini)
func TestE2E_AGY_MultiTurn(t *testing.T) {
	turn := 1
	backend := &mockE2EBackend{
		id: "agy_backend",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			if turn == 1 {
				return []types.StreamChunk{
					{
						ID: "gemini_1",
						ToolCalls: []types.ToolCall{
							{ID: "call_agy_run", Name: "run_command", Arguments: `{"command":"go version"}`},
						},
						FinishReason: "tool_calls",
						Done:         true,
					},
				}
			}
			return []types.StreamChunk{
				{ID: "gemini_2", Content: "Go version is go1.24.0", Done: true},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	// Turn 1
	body1, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "Check go version"}}},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{"name": "run_command", "parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
				},
			},
		},
	})
	req1 := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w1, req1, pool)
	resp1 := w1.Body.String()
	if !strings.Contains(resp1, "functionCall") || !strings.Contains(resp1, "run_command") {
		t.Fatalf("AGY Turn 1 expected functionCall, got: %s", resp1)
	}

	// Turn 2
	turn = 2
	body2, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "Check go version"}}},
			{"role": "model", "parts": []map[string]any{
				{"functionCall": map[string]any{"name": "run_command", "args": map[string]any{"command": "go version"}}},
			}},
			{"role": "function", "parts": []map[string]any{
				{"functionResponse": map[string]any{"name": "run_command", "response": map[string]any{"output": "go version go1.24.0 darwin/arm64"}}},
			}},
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w2, req2, pool)
	resp2 := w2.Body.String()
	if !strings.Contains(resp2, "Go version is go1.24.0") {
		t.Fatalf("AGY Turn 2 expected completion, got: %s", resp2)
	}
}

// 4. E2E Multi-turn test for OpenAI Codex CLI (/v1/responses)
func TestE2E_Codex_MultiTurn(t *testing.T) {
	turn := 1
	backend := &mockE2EBackend{
		id: "codex_backend",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			if turn == 1 {
				return []types.StreamChunk{
					{
						ID: "codex_1",
						ToolCalls: []types.ToolCall{
							{ID: "call_exec_1", Name: "exec_command", Arguments: `{"command":"pwd"}`},
						},
						FinishReason: "tool_calls",
						Done:         true,
					},
				}
			}
			return []types.StreamChunk{
				{ID: "codex_2", Content: "Working directory is /app", Done: true},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	// Turn 1
	body1, _ := json.Marshal(map[string]any{
		"model":  "gpt-5.1-codex",
		"stream": true,
		"input": []map[string]any{
			{"role": "user", "content": "Print current directory"},
		},
		"tools": []map[string]any{
			{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
		},
	})
	req1 := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w1, req1, pool)
	resp1 := w1.Body.String()
	if !strings.Contains(resp1, "response.output_item.added") || !strings.Contains(resp1, "exec_command") {
		t.Fatalf("Codex Turn 1 expected output_item with exec_command, got: %s", resp1)
	}

	// Turn 2
	turn = 2
	body2, _ := json.Marshal(map[string]any{
		"model":  "gpt-5.1-codex",
		"stream": true,
		"input": []map[string]any{
			{"role": "user", "content": "Print current directory"},
			{"type": "function_call", "call_id": "call_exec_1", "name": "exec_command", "arguments": `{"command":"pwd"}`},
			{"type": "function_call_output", "call_id": "call_exec_1", "output": "/app\n"},
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w2, req2, pool)
	resp2 := w2.Body.String()
	if !strings.Contains(resp2, "Working directory is /app") {
		t.Fatalf("Codex Turn 2 expected completion text, got: %s", resp2)
	}
}
