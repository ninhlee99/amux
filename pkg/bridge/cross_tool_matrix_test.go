package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// mockCrossBackend records received ChatRequest and produces mock StreamChunk stream.
type mockCrossBackend struct {
	id       string
	priority int
	group    string
	lastReq  *types.ChatRequest
	onSend   func(req *types.ChatRequest) []types.StreamChunk
}

func (m *mockCrossBackend) ID() string    { return m.id }
func (m *mockCrossBackend) Priority() int { return m.priority }
func (m *mockCrossBackend) Group() string { return m.group }
func (m *mockCrossBackend) SupportsTools() bool { return true }

func (m *mockCrossBackend) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	m.lastReq = req
	chunks := m.onSend(req)
	ch := make(chan types.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// ----------------------------------------------------------------------------
// 1. Claude Client -> Codex Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_ClaudeClient_CodexBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "codex:01",
		priority: 1,
		group:    "codex_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			// Backend emits tool call
			return []types.StreamChunk{
				{
					ID: "codex:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_bash_123",
							Name:      "Bash",
							Arguments: `{"command":"git status"}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// Claude client sends /v1/messages
	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": []map[string]any{
			{
				"name":        "Bash",
				"description": "Run shell command",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Check git status"},
		},
	}
	b, _ := json.Marshal(claudeBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	// Verify Claude client received Anthropic tool_use SSE
	respStr := w.Body.String()
	if !strings.Contains(respStr, `"type":"tool_use"`) || !strings.Contains(respStr, `"name":"Bash"`) || !strings.Contains(respStr, `"call_bash_123"`) {
		t.Fatalf("Claude client did not receive expected tool_use: %s", respStr)
	}

	// Turn 2: Claude client sends tool_result
	claudeTurn2 := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools":  claudeBody["tools"],
		"messages": []map[string]any{
			{"role": "user", "content": "Check git status"},
			{
				"role": "assistant",
				"content": []map[string]any{
					{
						"type":  "tool_use",
						"id":    "call_bash_123",
						"name":  "Bash",
						"input": map[string]any{"command": "git status"},
					},
				},
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type":         "tool_result",
						"tool_use_id": "call_bash_123",
						"content":      "On branch main, nothing to commit",
					},
				},
			},
		},
	}
	b2, _ := json.Marshal(claudeTurn2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b2))
	w2 := httptest.NewRecorder()

	backend.onSend = func(r *types.ChatRequest) []types.StreamChunk {
		return []types.StreamChunk{
			{ID: "codex:01", Content: "Repository is clean.", Done: true, FinishReason: "stop"},
		}
	}

	if err := bridge.HandleClaudeMessages(w2, req2, pool, b2); err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}

	// Verify backend received canonical Role: tool with ToolCallID: call_bash_123
	foundToolResult := false
	for _, m := range backend.lastReq.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_bash_123" {
			foundToolResult = true
			if !strings.Contains(m.Content, "On branch main") {
				t.Errorf("unexpected content: %s", m.Content)
			}
		}
	}
	if !foundToolResult {
		t.Fatalf("backend did not receive tool_result turn: %+v", backend.lastReq.Messages)
	}

	// Verify Codex Responses format can encode this cleanly
	codexEncoded, err := tools.MarshalCodexResponsesRequest(backend.lastReq)
	if err != nil {
		t.Fatalf("MarshalCodexResponsesRequest failed: %v", err)
	}
	if !strings.Contains(string(codexEncoded), `"function_call_output"`) || !strings.Contains(string(codexEncoded), "call_bash_123") {
		t.Fatalf("Codex payload missing function_call_output: %s", codexEncoded)
	}
}

// ----------------------------------------------------------------------------
// 2. Codex Client -> Claude Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_CodexClient_ClaudeBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "claude:sub:01",
		priority: 1,
		group:    "claude_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "claude:sub:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "toolu_exec_999",
							Name:      "exec_command",
							Arguments: `{"cmd":"pwd"}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// Codex client sends /v1/responses
	codexBody := map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"tools": []map[string]any{
			{
				"type":        "function",
				"name":        "exec_command",
				"description": "Execute local shell command",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cmd": map[string]any{"type": "string"},
					},
				},
			},
		},
		"input": []map[string]any{
			{"role": "user", "content": "Print working directory"},
		},
	}
	b, _ := json.Marshal(codexBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleOpenAIResponses status=%d: %s", w.Code, w.Body.String())
	}

	respStr := w.Body.String()
	if !strings.Contains(respStr, "response.output_item.added") || !strings.Contains(respStr, `"exec_command"`) || !strings.Contains(respStr, "toolu_exec_999") {
		t.Fatalf("Codex client missing function_call item: %s", respStr)
	}

	// Turn 2: Codex client returns function_call_output
	codexTurn2 := map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"tools":  codexBody["tools"],
		"input": []map[string]any{
			{"role": "user", "content": "Print working directory"},
			{
				"type":      "function_call",
				"name":      "exec_command",
				"call_id":   "toolu_exec_999",
				"arguments": `{"cmd":"pwd"}`,
			},
			{
				"type":    "function_call_output",
				"call_id": "toolu_exec_999",
				"output":  "/Users/ninh.le/workspace",
			},
		},
	}
	b2, _ := json.Marshal(codexTurn2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b2))
	w2 := httptest.NewRecorder()

	backend.onSend = func(r *types.ChatRequest) []types.StreamChunk {
		return []types.StreamChunk{
			{ID: "claude:sub:01", Content: "You are in /Users/ninh.le/workspace", Done: true, FinishReason: "stop"},
		}
	}

	bridge.HandleOpenAIResponses(w2, req2, pool)
	if w2.Code != http.StatusOK {
		t.Fatalf("Turn 2 status=%d: %s", w2.Code, w2.Body.String())
	}

	// Verify Claude request formatting
	claudePayload, err := tools.MarshalClaudeMessagesRequest(backend.lastReq, "claude-3-7-sonnet-20250219")
	if err != nil {
		t.Fatalf("MarshalClaudeMessagesRequest failed: %v", err)
	}
	if !strings.Contains(string(claudePayload), `"tool_result"`) || !strings.Contains(string(claudePayload), "toolu_exec_999") {
		t.Fatalf("Claude payload missing tool_result: %s", claudePayload)
	}
}

// ----------------------------------------------------------------------------
// 3. AGY Client -> Claude Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_AGYClient_ClaudeBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "claude:sub:01",
		priority: 1,
		group:    "claude_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "claude:sub:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "toolu_read_555",
							Name:      "read_file",
							Arguments: `{"path":"main.go"}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY client sends /v1beta/models/gemini-2.5-pro:streamGenerateContent
	agyBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Read file main.go"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "read_file",
						"description": "Read file contents",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"path": map[string]any{"type": "STRING"},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	respStr := w.Body.String()
	if !strings.Contains(respStr, `"functionCall"`) || !strings.Contains(respStr, `"read_file"`) {
		t.Fatalf("AGY client missing functionCall part: %s", respStr)
	}

	// Verify schema was normalized from OBJECT to object
	if len(backend.lastReq.Tools) != 1 {
		t.Fatalf("expected 1 tool in backend req, got: %+v", backend.lastReq.Tools)
	}
	schemaStr := string(backend.lastReq.Tools[0].InputSchema)
	if strings.Contains(schemaStr, "OBJECT") || !strings.Contains(schemaStr, "object") {
		t.Fatalf("expected lowercase object schema, got: %s", schemaStr)
	}
}

// ----------------------------------------------------------------------------
// 4. AGY Client -> Codex Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_AGYClient_CodexBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "codex:01",
		priority: 1,
		group:    "codex_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "codex:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_list_777",
							Name:      "list_dir",
							Arguments: `{"dir":"."}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY client sends /v1beta/models/gemini-2.5-pro:streamGenerateContent
	agyBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "List current directory"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "list_dir",
						"description": "List files in directory",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"dir": map[string]any{"type": "STRING"},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	respStr := w.Body.String()
	if !strings.Contains(respStr, `"functionCall"`) || !strings.Contains(respStr, `"list_dir"`) {
		t.Fatalf("AGY client missing functionCall part: %s", respStr)
	}

	// Turn 2: AGY returns functionResponse
	agyTurn2 := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "List current directory"},
				},
			},
			{
				"role": "model",
				"parts": []map[string]any{
					{
						"functionCall": map[string]any{
							"name": "list_dir",
							"args": map[string]any{"dir": "."},
						},
					},
				},
			},
			{
				"role": "tool",
				"parts": []map[string]any{
					{
						"functionResponse": map[string]any{
							"name": "list_dir",
							"response": map[string]any{
								"output": "fileA, fileB",
							},
						},
					},
				},
			},
		},
		"tools": agyBody["tools"],
	}
	b2, _ := json.Marshal(agyTurn2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", bytes.NewReader(b2))
	w2 := httptest.NewRecorder()

	backend.onSend = func(r *types.ChatRequest) []types.StreamChunk {
		return []types.StreamChunk{
			{ID: "codex:01", Content: "Found fileA and fileB.", Done: true, FinishReason: "stop"},
		}
	}

	bridge.HandleGeminiGenerateContent(w2, req2, pool)
	if w2.Code != http.StatusOK {
		t.Fatalf("Turn 2 status=%d: %s", w2.Code, w2.Body.String())
	}

	// Verify Codex Responses format can encode this turn
	codexPayload, err := tools.MarshalCodexResponsesRequest(backend.lastReq)
	if err != nil {
		t.Fatalf("MarshalCodexResponsesRequest failed: %v", err)
	}
	if !strings.Contains(string(codexPayload), `"function_call_output"`) {
		t.Fatalf("Codex payload missing function_call_output: %s", codexPayload)
	}
}

// ----------------------------------------------------------------------------
// 5. Codex Client -> AGY Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_CodexClient_AGYBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "agy:sub:01",
		priority: 1,
		group:    "agy_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "agy:sub:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_grep_444",
							Name:      "grep_search",
							Arguments: `{"pattern":"func main"}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	codexBody := map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "grep_search",
					"description": "Search code pattern",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"pattern": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Find main function"},
		},
	}
	b, _ := json.Marshal(codexBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleChatCompletions(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleChatCompletions status=%d: %s", w.Code, w.Body.String())
	}

	respStr := w.Body.String()
	if !strings.Contains(respStr, `"tool_calls"`) || !strings.Contains(respStr, `"grep_search"`) {
		t.Fatalf("Codex client missing tool_calls: %s", respStr)
	}

	// Verify AGY tools format can encode this
	geminiFns := tools.ToGeminiFunctions(backend.lastReq.Tools)
	if len(geminiFns) != 1 || geminiFns[0].Name != "grep_search" {
		t.Fatalf("expected gemini function grep_search, got: %+v", geminiFns)
	}
}

// ----------------------------------------------------------------------------
// 6. Claude Client -> AGY Backend
// ----------------------------------------------------------------------------
func TestCrossMatrix_ClaudeClient_AGYBackend(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "agy:sub:01",
		priority: 1,
		group:    "agy_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "agy:sub:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "toolu_subagent_888",
							Name:      "invoke_subagent",
							Arguments: `{"role":"researcher","prompt":"investigate issue"}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": []map[string]any{
			{
				"name":        "invoke_subagent",
				"description": "Invoke subagent",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"role":   map[string]any{"type": "string"},
						"prompt": map[string]any{"type": "string"},
					},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Spawn researcher subagent"},
		},
	}
	b, _ := json.Marshal(claudeBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	respStr := w.Body.String()
	if !strings.Contains(respStr, `"type":"tool_use"`) || !strings.Contains(respStr, `"name":"invoke_subagent"`) {
		t.Fatalf("Claude client did not receive tool_use: %s", respStr)
	}

	// Verify AGY tools format can encode this
	geminiFns := tools.ToGeminiFunctions(backend.lastReq.Tools)
	if len(geminiFns) != 1 || geminiFns[0].Name != "invoke_subagent" {
		t.Fatalf("expected gemini function invoke_subagent, got: %+v", geminiFns)
	}
}

// ----------------------------------------------------------------------------
// 7. Codex Client -> Non-String Object Output in function_call_output
// ----------------------------------------------------------------------------
func TestCrossMatrix_CodexClient_NonStringOutput(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "claude:sub:01",
		priority: 1,
		group:    "claude_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{ID: "claude:sub:01", Content: "Received output", Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	codexBody := map[string]any{
		"model":  "gpt-5-codex",
		"stream": false,
		"input": []map[string]any{
			{"role": "user", "content": "List files"},
			{
				"type":      "function_call",
				"name":      "list_dir",
				"call_id":   "call_list_dir_1",
				"arguments": `{"dir":"."}`,
			},
			{
				"type":    "function_call_output",
				"call_id": "call_list_dir_1",
				"output": map[string]any{
					"files":   []string{"fileA.go", "fileB.go"},
					"success": true,
				},
			},
		},
	}
	b, _ := json.Marshal(codexBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleOpenAIResponses status=%d: %s", w.Code, w.Body.String())
	}

	foundTool := false
	for _, m := range backend.lastReq.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_list_dir_1" {
			foundTool = true
			if !strings.Contains(m.Content, "fileA.go") || !strings.Contains(m.Content, "fileB.go") {
				t.Fatalf("expected serialized JSON object in tool content, got: %s", m.Content)
			}
		}
	}
	if !foundTool {
		t.Fatalf("backend did not receive tool turn: %+v", backend.lastReq.Messages)
	}
}

// ----------------------------------------------------------------------------
// 8. Web Backend -> AGY Client: Tool Aliases & Argument Coercion
// ----------------------------------------------------------------------------
func TestCrossMatrix_WebBackend_AGYClient_ToolCoercion(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			// Web model outputs Edit tool call with standard Claude arguments
			xml := `<tool_call>
{"name":"Edit","arguments":{"file_path":"pkg/main.go","old_string":"func Old()","new_string":"func New()"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY client sends /v1beta request with AGY's replace_file_content tool
	agyBody := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "Replace function"}}},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "replace_file_content",
						"description": "Edit file",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"TargetFile":         map[string]any{"type": "STRING"},
								"TargetContent":      map[string]any{"type": "STRING"},
								"ReplacementContent": map[string]any{"type": "STRING"},
								"AllowMultiple":      map[string]any{"type": "BOOLEAN"},
							},
							"required": []string{"TargetFile", "TargetContent", "ReplacementContent"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v (body: %s)", err, w.Body.String())
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		t.Fatalf("expected candidate parts, got: %s", w.Body.String())
	}
	var fc *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.FunctionCall != nil {
			fc = p.FunctionCall
			break
		}
	}
	if fc == nil {
		t.Fatalf("expected functionCall part in response, got: %s", w.Body.String())
	}

	// Verify alias mapped Edit -> replace_file_content
	if fc.Name != "replace_file_content" {
		t.Errorf("expected function name replace_file_content, got: %s", fc.Name)
	}
	// Verify arguments coerced from file_path/old_string/new_string to TargetFile/TargetContent/ReplacementContent
	if fc.Args["TargetFile"] != "pkg/main.go" {
		t.Errorf("expected TargetFile pkg/main.go, got: %v", fc.Args["TargetFile"])
	}
	if fc.Args["TargetContent"] != "func Old()" {
		t.Errorf("expected TargetContent func Old(), got: %v", fc.Args["TargetContent"])
	}
	if fc.Args["ReplacementContent"] != "func New()" {
		t.Errorf("expected ReplacementContent func New(), got: %v", fc.Args["ReplacementContent"])
	}
	// Verify default AllowMultiple is set
	if fc.Args["AllowMultiple"] != false {
		t.Errorf("expected AllowMultiple false, got: %v", fc.Args["AllowMultiple"])
	}
}

// ----------------------------------------------------------------------------
// 9. Web Backend -> Claude Client: Write tool alias & MCP tool
// ----------------------------------------------------------------------------
func TestCrossMatrix_WebBackend_ClaudeClient_ToolAndMCP(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "gemini:web:01",
		priority: 1,
		group:    "gemini_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"write_to_file","arguments":{"TargetFile":"config.json","CodeContent":"{\"active\":true}"}}
</tool_call>
<tool_call>
{"name":"github_create_issue","arguments":{"title":"Bug report","body":"Found error"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "gemini:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": false,
		"tools": []map[string]any{
			{
				"name":        "Write",
				"description": "Write file",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string"},
						"content":   map[string]any{"type": "string"},
					},
					"required": []string{"file_path", "content"},
				},
			},
			{
				"name":        "mcp__github__create_issue",
				"description": "Create issue on GitHub via MCP",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{"type": "string"},
						"body":  map[string]any{"type": "string"},
					},
					"required": []string{"title", "body"},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Write config and report issue"},
		},
	}
	b, _ := json.Marshal(claudeBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	var resp struct {
		Content []struct {
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal claude resp: %v (body: %s)", err, w.Body.String())
	}

	if len(resp.Content) < 2 {
		t.Fatalf("expected at least 2 content blocks, got: %+v", resp.Content)
	}

	foundWrite := false
	foundMCP := false
	for _, block := range resp.Content {
		if block.Type != "tool_use" {
			continue
		}
		if block.Name == "Write" {
			foundWrite = true
			if block.Input["file_path"] != "config.json" {
				t.Errorf("expected file_path config.json, got: %v", block.Input["file_path"])
			}
			if block.Input["content"] != `{"active":true}` {
				t.Errorf("expected content, got: %v", block.Input["content"])
			}
		}
		if block.Name == "mcp__github__create_issue" {
			foundMCP = true
			if block.Input["title"] != "Bug report" {
				t.Errorf("expected title Bug report, got: %v", block.Input["title"])
			}
		}
	}

	if !foundWrite {
		t.Errorf("Write tool_use block not found: %+v", resp.Content)
	}
	if !foundMCP {
		t.Errorf("MCP tool_use block not found: %+v", resp.Content)
	}
}

// ----------------------------------------------------------------------------
// 10. Web Backend -> Codex Client (Responses API): Tool Aliases & Coercion
// ----------------------------------------------------------------------------
func TestCrossMatrix_WebBackend_CodexClient_ToolCoercion(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"replace_file_content","arguments":{"TargetFile":"server.go","TargetContent":"oldCode","ReplacementContent":"newCode"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	codexBody := map[string]any{
		"model":  "gpt-5-codex",
		"stream": false,
		"tools": []map[string]any{
			{
				"type": "function",
				"name": "edit_file",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string"},
						"old_str": map[string]any{"type": "string"},
						"new_str": map[string]any{"type": "string"},
					},
					"required": []string{"path", "old_str", "new_str"},
				},
			},
		},
		"input": []map[string]any{
			{"role": "user", "content": "Update server.go"},
		},
	}
	b, _ := json.Marshal(codexBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleOpenAIResponses(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleOpenAIResponses status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Output []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal codex resp: %v (body: %s)", err, w.Body.String())
	}

	foundEdit := false
	for _, item := range resp.Output {
		if item.Type == "function_call" && item.Name == "edit_file" {
			foundEdit = true
			var args map[string]any
			if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
				t.Fatalf("unmarshal args: %v", err)
			}
			if args["path"] != "server.go" {
				t.Errorf("expected path server.go, got: %v", args["path"])
			}
			if args["old_str"] != "oldCode" {
				t.Errorf("expected old_str oldCode, got: %v", args["old_str"])
			}
			if args["new_str"] != "newCode" {
				t.Errorf("expected new_str newCode, got: %v", args["new_str"])
			}
		}
	}
	if !foundEdit {
		t.Fatalf("expected function_call for edit_file in output, got: %+v", resp.Output)
	}
}

// ----------------------------------------------------------------------------
// 11. Subagent Cross-Conversion: Claude (Agent) <-> AGY (invoke_subagent)
// ----------------------------------------------------------------------------
func TestCrossMatrix_Subagent_ClaudeClient_AGYBackend(t *testing.T) {
	// Backend generates AGY-style invoke_subagent
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"invoke_subagent","arguments":{"Subagents":[{"TypeName":"research","Role":"Researcher","Prompt":"Find auth logic"}]}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// Claude Client sends /v1/messages with "Agent" tool
	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": false,
		"tools": []map[string]any{
			{
				"name":        "Agent",
				"description": "Run a subagent",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"prompt":      map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"prompt"},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Delegate research to a subagent"},
		},
	}
	b, _ := json.Marshal(claudeBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	var resp struct {
		Content []struct {
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v (body: %s)", err, w.Body.String())
	}

	foundAgent := false
	for _, c := range resp.Content {
		if c.Type == "tool_use" && c.Name == "Agent" {
			foundAgent = true
			if c.Input["prompt"] != "Find auth logic" {
				t.Errorf("expected prompt 'Find auth logic', got: %v", c.Input["prompt"])
			}
			if c.Input["description"] != "Researcher" {
				t.Errorf("expected description 'Researcher', got: %v", c.Input["description"])
			}
		}
	}
	if !foundAgent {
		t.Fatalf("Agent tool_use not found in response: %+v", resp.Content)
	}
}

func TestCrossMatrix_Subagent_AGYClient_ClaudeBackend(t *testing.T) {
	// Backend generates Claude-style Agent tool call
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"Agent","arguments":{"prompt":"Implement oauth flow","description":"OAuth expert"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY Client calls /v1beta/models/gemini-2.5-pro:generateContent with invoke_subagent
	agyBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Spawn oauth subagent"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "invoke_subagent",
						"description": "Invoke subagents",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"Subagents":   map[string]any{"type": "ARRAY"},
								"toolAction":  map[string]any{"type": "STRING"},
								"toolSummary": map[string]any{"type": "STRING"},
							},
							"required": []string{"Subagents", "toolAction", "toolSummary"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []map[string]any `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	foundSubagent := false
	for _, part := range resp.Candidates[0].Content.Parts {
		if fc, ok := part["functionCall"].(map[string]any); ok {
			if fc["name"] == "invoke_subagent" {
				foundSubagent = true
				args, _ := fc["args"].(map[string]any)
				subs, _ := args["Subagents"].([]any)
				if len(subs) == 0 {
					t.Fatalf("expected non-empty Subagents array, got: %+v", args)
				}
				sub0, _ := subs[0].(map[string]any)
				if sub0["Prompt"] != "Implement oauth flow" {
					t.Errorf("expected Prompt 'Implement oauth flow', got: %v", sub0["Prompt"])
				}
				if args["toolAction"] == "" || args["toolSummary"] == "" {
					t.Errorf("expected toolAction and toolSummary to be populated, got: %+v", args)
				}
			}
		}
	}
	if !foundSubagent {
		t.Fatalf("invoke_subagent functionCall not found: %+v", resp.Candidates[0].Content.Parts)
	}
}

// ----------------------------------------------------------------------------
// 12. MCP Cross-Conversion: Claude (mcp__*) <-> AGY (call_mcp_tool)
// ----------------------------------------------------------------------------
func TestCrossMatrix_MCP_ClaudeClient_AGYBackend(t *testing.T) {
	// Backend generates AGY call_mcp_tool
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"call_mcp_tool","arguments":{"ServerName":"filesystem","ToolName":"read_file","Arguments":{"path":"/etc/hosts"}}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// Claude Client sends mcp__filesystem__read_file
	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": false,
		"tools": []map[string]any{
			{
				"name":        "mcp__filesystem__read_file",
				"description": "Read file via MCP",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "Read hosts file via MCP"},
		},
	}
	b, _ := json.Marshal(claudeBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	var resp struct {
		Content []struct {
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	foundMCP := false
	for _, c := range resp.Content {
		if c.Type == "tool_use" && c.Name == "mcp__filesystem__read_file" {
			foundMCP = true
			if c.Input["path"] != "/etc/hosts" {
				t.Errorf("expected path '/etc/hosts', got: %v", c.Input["path"])
			}
		}
	}
	if !foundMCP {
		t.Fatalf("mcp__filesystem__read_file tool_use not found: %+v", resp.Content)
	}
}

func TestCrossMatrix_MCP_AGYClient_ClaudeBackend(t *testing.T) {
	// Backend generates Claude-style mcp__filesystem__read_file
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"mcp__filesystem__read_file","arguments":{"path":"/etc/hosts"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY Client calls Gemini endpoint with call_mcp_tool
	agyBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": "Call MCP tool"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "call_mcp_tool",
						"description": "Call MCP tool",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"ServerName":  map[string]any{"type": "STRING"},
								"ToolName":    map[string]any{"type": "STRING"},
								"Arguments":   map[string]any{"type": "OBJECT"},
								"toolAction":  map[string]any{"type": "STRING"},
								"toolSummary": map[string]any{"type": "STRING"},
							},
							"required": []string{"ServerName", "ToolName", "Arguments", "toolAction", "toolSummary"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []map[string]any `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	foundCallMCP := false
	for _, part := range resp.Candidates[0].Content.Parts {
		if fc, ok := part["functionCall"].(map[string]any); ok {
			if fc["name"] == "call_mcp_tool" {
				foundCallMCP = true
				args, _ := fc["args"].(map[string]any)
				if args["ServerName"] != "filesystem" {
					t.Errorf("expected ServerName 'filesystem', got: %v", args["ServerName"])
				}
				if args["ToolName"] != "read_file" {
					t.Errorf("expected ToolName 'read_file', got: %v", args["ToolName"])
				}
				if args["toolAction"] == "" || args["toolSummary"] == "" {
					t.Errorf("expected toolAction and toolSummary to be populated, got: %+v", args)
				}
			}
		}
	}
	if !foundCallMCP {
		t.Fatalf("call_mcp_tool functionCall not found: %+v", resp.Candidates[0].Content.Parts)
	}
}

// ----------------------------------------------------------------------------
// 13. Multi-Turn Agent Loop & 25k Token Budget Limit Stress Test
// ----------------------------------------------------------------------------
func TestCrossMatrix_MultiTurnAgentLoop_TokenLimitGuarantee(t *testing.T) {
	// Build a 10-turn conversation with massive tool outputs (total > 60,000 tokens)
	var messages []types.ChatMessage
	messages = append(messages, types.ChatMessage{
		Role:    "system",
		Content: "You are an autonomous AI software engineer. Follow rules strictly.",
	})
	messages = append(messages, types.ChatMessage{
		Role:    "user",
		Content: "Diagnose and fix production crash in cluster deployment.",
	})

	// Add 9 subsequent turns of tool calls and giant tool outputs (15,000 chars each)
	toolNames := []string{"Bash", "read_file", "grep_search", "find_by_name", "edit_file"}
	for i := 1; i <= 8; i++ {
		toolName := toolNames[i%len(toolNames)]
		messages = append(messages, types.ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("Running step %d with %s", i, toolName),
			ToolCalls: []types.ToolCall{
				{
					ID:        fmt.Sprintf("call_%s_%d", toolName, i),
					Name:      toolName,
					Arguments: fmt.Sprintf(`{"command":"step_%d","path":"file_%d.go"}`, i, i),
				},
			},
		})
		// Giant output (15,000 runes each = ~4,000 tokens each x 8 = 32,000 tokens)
		giantLog := fmt.Sprintf("=== Step %d Log ===\n", i) + strings.Repeat("STACK TRACE LINE OK [INFO] 0xDEADBEEF\n", 400)
		messages = append(messages, types.ChatMessage{
			Role:       "user",
			Content:    giantLog,
			ToolCallID: fmt.Sprintf("call_%s_%d", toolName, i),
		})
	}

	messages = append(messages, types.ChatMessage{
		Role:    "user",
		Content: "Now give me the final root cause analysis and resolution.",
	})

	req := &types.ChatRequest{
		Model:    "chatgpt:web:01",
		Messages: messages,
		Tools: []types.ToolDef{
			{Name: "Bash", InputSchema: []byte(`{"type":"object"}`)},
		},
	}

	// Verify that provider.WebBackendPrompt guarantees:
	// 1. Never exceeds DefaultWebMaxTokens (20,000 tokens)
	// 2. Never exceeds AbsoluteMaxWebRunes (85,000 runes)
	// 3. ChatGPT Web, Claude Web, Gemini Web will NEVER hit 413 Payload Too Large!
	prompt := bridgeTestWebBackendPrompt(req)

	runeCount := len([]rune(prompt))
	estTokens := runeCount / 4

	if runeCount > 85000 {
		t.Fatalf("CRITICAL: Web backend prompt exceeded AbsoluteMaxWebRunes (85000): got %d runes", runeCount)
	}
	if estTokens > 22000 {
		t.Fatalf("CRITICAL: Web backend prompt exceeded safe 20k token limit: est %d tokens", estTokens)
	}

	// Verify preservation of initial goal and recent tail
	if !strings.Contains(prompt, "Diagnose and fix production crash") {
		t.Errorf("Expected prompt to preserve initial user goal, but it was lost!")
	}
	if !strings.Contains(prompt, "final root cause analysis") {
		t.Errorf("Expected prompt to preserve the latest user turn, but it was lost!")
	}
}

func bridgeTestWebBackendPrompt(req *types.ChatRequest) string {
	return provider.WebBackendPrompt(req, false)
}

// ----------------------------------------------------------------------------
// 14. Edge Cases: Type Coercion for String Booleans, Numbers & Single-Item Arrays
// ----------------------------------------------------------------------------
func TestCrossMatrix_TypeCoercion_StringNumbersAndBooleans(t *testing.T) {
	// Web model generates numbers and booleans as strings, and path as single item array:
	// {"TargetFile":["main.go"],"TargetContent":"foo","ReplacementContent":"bar","StartLine":"10","EndLine":"25","Overwrite":"true","AllowMultiple":"false","WaitMsBeforeAsync":"3000"}
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"replace_file_content","arguments":{"TargetFile":["main.go"],"TargetContent":"foo","ReplacementContent":"bar","StartLine":"10","EndLine":"25","Overwrite":"true","AllowMultiple":"false","WaitMsBeforeAsync":"3000"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY Client sends replace_file_content expecting typed integers and booleans
	agyBody := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "Replace code in main.go"}}},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "replace_file_content",
						"description": "Replace file content",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"TargetFile":         map[string]any{"type": "STRING"},
								"TargetContent":      map[string]any{"type": "STRING"},
								"ReplacementContent": map[string]any{"type": "STRING"},
								"StartLine":          map[string]any{"type": "INTEGER"},
								"EndLine":            map[string]any{"type": "INTEGER"},
								"Overwrite":          map[string]any{"type": "BOOLEAN"},
								"AllowMultiple":      map[string]any{"type": "BOOLEAN"},
								"WaitMsBeforeAsync":  map[string]any{"type": "INTEGER"},
								"toolAction":         map[string]any{"type": "STRING"},
								"toolSummary":        map[string]any{"type": "STRING"},
								"Instruction":        map[string]any{"type": "STRING"},
								"Description":        map[string]any{"type": "STRING"},
							},
							"required": []string{
								"TargetFile", "TargetContent", "ReplacementContent",
								"StartLine", "EndLine", "AllowMultiple",
								"toolAction", "toolSummary", "Instruction", "Description",
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []map[string]any `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	foundTool := false
	for _, part := range resp.Candidates[0].Content.Parts {
		if fc, ok := part["functionCall"].(map[string]any); ok {
			if fc["name"] == "replace_file_content" {
				foundTool = true
				args, _ := fc["args"].(map[string]any)

				// Verify TargetFile is unwrapped to string (not array)
				if targetFile, ok := args["TargetFile"].(string); !ok || targetFile != "main.go" {
					t.Errorf("expected TargetFile 'main.go', got: %v (type %T)", args["TargetFile"], args["TargetFile"])
				}

				// Verify StartLine and EndLine coerced to float64/int (JSON unmarshals numbers as float64 in any)
				if sl, ok := args["StartLine"].(float64); !ok || int(sl) != 10 {
					t.Errorf("expected StartLine 10 (int/float), got: %v (type %T)", args["StartLine"], args["StartLine"])
				}
				if el, ok := args["EndLine"].(float64); !ok || int(el) != 25 {
					t.Errorf("expected EndLine 25 (int/float), got: %v (type %T)", args["EndLine"], args["EndLine"])
				}

				// Verify Overwrite and AllowMultiple coerced to real boolean
				if ow, ok := args["Overwrite"].(bool); !ok || ow != true {
					t.Errorf("expected Overwrite true (bool), got: %v (type %T)", args["Overwrite"], args["Overwrite"])
				}
				if am, ok := args["AllowMultiple"].(bool); !ok || am != false {
					t.Errorf("expected AllowMultiple false (bool), got: %v (type %T)", args["AllowMultiple"], args["AllowMultiple"])
				}

				// Verify required metadata fields populated
				if args["toolAction"] == "" || args["toolSummary"] == "" || args["Instruction"] == "" || args["Description"] == "" {
					t.Errorf("missing required schema strings: %+v", args)
				}
			}
		}
	}
	if !foundTool {
		t.Fatalf("replace_file_content functionCall not found: %+v", resp.Candidates[0].Content.Parts)
	}
}

// ----------------------------------------------------------------------------
// Universal Data Format: arbitrary/dynamic custom tool schema (never
// hardcoded anywhere in the engine) plus merchant/domain context embedded as
// XML tags and Shopify GID strings inside the prompt, round-tripped through
// Claude client -> canonical ChatRequest -> Codex backend -> Codex Responses
// wire format. Every nested schema field, and the raw XML/GID text, must
// survive byte-for-byte with zero silent redaction or restructuring.
// ----------------------------------------------------------------------------
func TestCrossMatrix_DynamicSchemaAndMerchantContext_ClaudeClient_CodexBackend(t *testing.T) {
	// A tool schema invented purely for this test: nested object property,
	// an array of enum-constrained items, and additionalProperties:false —
	// none of this shape exists anywhere in the engine's Go structs.
	dynamicSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"merchant": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"store_gid":  map[string]any{"type": "string"},
					"variant_gid": map[string]any{"type": "string"},
				},
				"required": []any{"store_gid"},
			},
			"fulfillment_channels": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": []any{"pickup", "ship", "digital"}},
			},
		},
		"required":             []any{"merchant"},
		"additionalProperties": false,
	}

	backend := &mockCrossBackend{
		id:       "codex:01",
		priority: 1,
		group:    "codex_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "codex:01",
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_merchant_1",
							Name:      "merchant_lookup",
							Arguments: `{"merchant":{"store_gid":"gid://shopify/Shop/123","variant_gid":"gid://shopify/ProductVariant/456"},"fulfillment_channels":["pickup","ship"]}`,
						},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	merchantPrompt := "Please check inventory.\n" +
		"<merchant_data>{\"store\":\"gid://shopify/Shop/123\",\"note\":\"line1\\nline2 <b>bold</b>\"}</merchant_data>\n" +
		"```json\n{\"raw\":true}\n```"

	claudeBody := map[string]any{
		"model":  "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": []map[string]any{
			{
				"name":         "merchant_lookup",
				"description":  "Look up merchant/variant fulfillment info",
				"input_schema": dynamicSchema,
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": merchantPrompt},
		},
	}
	b, err := json.Marshal(claudeBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(w, req, pool, b); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	if backend.lastReq == nil {
		t.Fatal("backend never received a request")
	}

	// 1. Prompt text (XML tags, escaped quotes, newlines, markdown fence,
	// Shopify GIDs) must survive verbatim into the canonical request.
	var promptSeen string
	for _, m := range backend.lastReq.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "merchant_data") {
			promptSeen = m.Content
		}
	}
	if promptSeen != merchantPrompt {
		t.Fatalf("prompt content mutated in transit:\nwant: %q\ngot:  %q", merchantPrompt, promptSeen)
	}

	// 2. The dynamic schema must round-trip with zero field loss (nested
	// properties, required, enum, items, additionalProperties).
	if len(backend.lastReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(backend.lastReq.Tools))
	}
	var gotSchema map[string]any
	if err := json.Unmarshal(backend.lastReq.Tools[0].InputSchema, &gotSchema); err != nil {
		t.Fatalf("unmarshal round-tripped schema: %v", err)
	}
	// Re-marshal-then-unmarshal the original for a normalized comparison
	// (map key order / JSON number formatting is not semantically significant).
	origBytes, _ := json.Marshal(dynamicSchema)
	var wantSchema map[string]any
	_ = json.Unmarshal(origBytes, &wantSchema)
	// The engine's NormalizeJSONSchema only lowercases "type" strings and
	// backfills an empty properties map — it must not drop or reshape
	// anything else, so every original key must still be present with an
	// equivalent value.
	assertSchemaSubset(t, wantSchema, gotSchema)

	// 3. Privacy redaction must not touch merchant identifiers or the tool
	// schema/arguments — only auth/secret patterns.
	before := *backend.lastReq
	beforeMsgs := append([]types.ChatMessage(nil), backend.lastReq.Messages...)
	res := privacy.RedactChatRequest(backend.lastReq)
	if res.Len() != 0 {
		t.Fatalf("expected zero redaction hits on merchant context, got: %s", res.Summary())
	}
	for i, m := range backend.lastReq.Messages {
		if m.Content != beforeMsgs[i].Content {
			t.Fatalf("redaction mutated message content: %q -> %q", beforeMsgs[i].Content, m.Content)
		}
	}
	_ = before

	// 4. Codex Responses wire format must still carry the tool call and its
	// nested arguments intact for the next turn.
	codexEncoded, err := tools.MarshalCodexResponsesRequest(backend.lastReq)
	if err != nil {
		t.Fatalf("MarshalCodexResponsesRequest failed: %v", err)
	}
	if !strings.Contains(string(codexEncoded), "gid://shopify/Shop/123") {
		t.Fatalf("Codex payload lost merchant context: %s", codexEncoded)
	}
}

// assertSchemaSubset fails the test if any key/value in want is missing or
// different in got (recursively). got may have extra normalization-added
// keys (e.g. a backfilled empty "properties" map) that want doesn't.
func assertSchemaSubset(t *testing.T, want, got map[string]any) {
	t.Helper()
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("schema lost key %q: want %v", k, wv)
		}
		switch wvt := wv.(type) {
		case map[string]any:
			gvt, ok := gv.(map[string]any)
			if !ok {
				t.Fatalf("schema key %q changed type: want map, got %T", k, gv)
			}
			assertSchemaSubset(t, wvt, gvt)
		case []any:
			gvt, ok := gv.([]any)
			if !ok || len(gvt) != len(wvt) {
				t.Fatalf("schema key %q array mismatch: want %v, got %v", k, wvt, gv)
			}
			for i := range wvt {
				if wm, ok := wvt[i].(map[string]any); ok {
					gm, ok := gvt[i].(map[string]any)
					if !ok {
						t.Fatalf("schema key %q[%d] changed type: want map, got %T", k, i, gvt[i])
					}
					assertSchemaSubset(t, wm, gm)
					continue
				}
				if fmt.Sprint(wvt[i]) != fmt.Sprint(gvt[i]) {
					t.Fatalf("schema key %q[%d] mismatch: want %v, got %v", k, i, wvt[i], gvt[i])
				}
			}
		default:
			// "type" strings are lowercased by NormalizeJSONSchema; compare
			// case-insensitively for strings, exactly otherwise.
			ws, wIsStr := wv.(string)
			gs, gIsStr := gv.(string)
			if wIsStr && gIsStr {
				if !strings.EqualFold(ws, gs) {
					t.Fatalf("schema key %q mismatch: want %v, got %v", k, wv, gv)
				}
				continue
			}
			if fmt.Sprint(wv) != fmt.Sprint(gv) {
				t.Fatalf("schema key %q mismatch: want %v, got %v", k, wv, gv)
			}
		}
	}
}

// ----------------------------------------------------------------------------
// 21. Web Backend & Native Stream -> AGY Client: RunCommand Strict Coercion
// ----------------------------------------------------------------------------
func TestCrossMatrix_WebBackend_AGYClient_RunCommandCoercion(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "chatgpt:web:01",
		priority: 1,
		group:    "chatgpt_web",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			xml := `<tool_call>
{"name":"run_command","arguments":{"command":"git status"}}
</tool_call>`
			return []types.StreamChunk{
				{ID: "chatgpt:web:01", Content: xml, Done: true, FinishReason: "stop"},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	// AGY client with strict run_command declaration
	agyBody := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "check git"}}},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "run_command",
						"description": "PROPOSE a command to run",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"CommandLine":       map[string]any{"type": "STRING"},
								"Cwd":               map[string]any{"type": "STRING"},
								"WaitMsBeforeAsync": map[string]any{"type": "INTEGER"},
								"toolAction":        map[string]any{"type": "STRING"},
								"toolSummary":       map[string]any{"type": "STRING"},
							},
							"required": []string{"Cwd", "WaitMsBeforeAsync", "CommandLine", "toolSummary", "toolAction"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v (body: %s)", err, w.Body.String())
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		t.Fatalf("expected candidate parts, got: %s", w.Body.String())
	}

	var fc *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.FunctionCall != nil {
			fc = p.FunctionCall
			break
		}
	}
	if fc == nil {
		t.Fatalf("expected functionCall in response, got: %s", w.Body.String())
	}

	if fc.Name != "run_command" {
		t.Errorf("expected tool run_command, got %s", fc.Name)
	}

	// Verify CommandLine was normalized
	if fc.Args["CommandLine"] != "git status" {
		t.Errorf("expected CommandLine 'git status', got %v", fc.Args["CommandLine"])
	}

	// Verify 'command' was removed so AGY validator won't reject with 'additional properties: command not allowed'
	if _, hasCmd := fc.Args["command"]; hasCmd {
		t.Errorf("expected 'command' to be stripped from args, but still present: %v", fc.Args)
	}

	// Verify required AGY fields
	if fc.Args["Cwd"] != "." {
		t.Errorf("expected Cwd '.', got %v", fc.Args["Cwd"])
	}
	if v, ok := fc.Args["WaitMsBeforeAsync"].(float64); !ok || int(v) != 10000 {
		t.Errorf("expected WaitMsBeforeAsync 10000, got %v", fc.Args["WaitMsBeforeAsync"])
	}
	if fc.Args["toolAction"] == "" {
		t.Errorf("expected non-empty toolAction, got %v", fc.Args["toolAction"])
	}
	if fc.Args["toolSummary"] == "" {
		t.Errorf("expected non-empty toolSummary, got %v", fc.Args["toolSummary"])
	}
}

func TestCrossMatrix_NativeStream_AGYClient_BashToRunCommand(t *testing.T) {
	backend := &mockCrossBackend{
		id:       "codex:01",
		priority: 1,
		group:    "codex_sub",
		onSend: func(req *types.ChatRequest) []types.StreamChunk {
			return []types.StreamChunk{
				{
					ID: "codex:01",
					ToolCalls: []types.ToolCall{
						{ID: "call_native_1", Name: "Bash", Arguments: `{"command":"pwd"}`},
					},
					FinishReason: "tool_calls",
					Done:         true,
				},
			}
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})

	agyBody := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "check pwd"}}},
		},
		"tools": []map[string]any{
			{
				"functionDeclarations": []map[string]any{
					{
						"name":        "run_command",
						"description": "Run command",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"CommandLine":       map[string]any{"type": "STRING"},
								"Cwd":               map[string]any{"type": "STRING"},
								"WaitMsBeforeAsync": map[string]any{"type": "INTEGER"},
								"toolAction":        map[string]any{"type": "STRING"},
								"toolSummary":       map[string]any{"type": "STRING"},
							},
							"required": []string{"Cwd", "WaitMsBeforeAsync", "CommandLine", "toolSummary", "toolAction"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(agyBody)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	bridge.HandleGeminiGenerateContent(w, req, pool)
	if w.Code != http.StatusOK {
		t.Fatalf("HandleGeminiGenerateContent status=%d: %s", w.Code, w.Body.String())
	}

	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, `"name":"run_command"`) {
		t.Fatalf("expected stream to contain run_command, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"CommandLine":"pwd"`) {
		t.Fatalf("expected stream to contain CommandLine pwd, got:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, `"command":"pwd"`) {
		t.Fatalf("stream MUST NOT contain redundant 'command', got:\n%s", bodyStr)
	}
}



