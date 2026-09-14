package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type proxyRoundTripperFn func(req *http.Request) (*http.Response, error)

func (f proxyRoundTripperFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// ============================================================================
// 1. CLAUDE CODE THROUGH PROXY WITH CODEX (codex:01)
// Tests: Multi-Tools, Multi-Agents, Multi-SubAgents, Multi-Loop Agents & Tools
// ============================================================================
func TestProxy_ClaudeCode_Codex_MultiTools_MultiAgent_MultiSubAgent_MultiLoop(t *testing.T) {
	tmpDir := t.TempDir()
	authDoc := map[string]any{
		"tokens": map[string]any{
			"access_token":  "mock-jwt.eyJleHAiOjk5OTk5OTk5OTl9.sig",
			"refresh_token": "mock-refresh",
			"account_id":    "codex-acc-1",
		},
	}
	authBytes, _ := json.Marshal(authDoc)
	_ = os.MkdirAll(filepath.Join(tmpDir, ".codex"), 0o755)
	_ = os.WriteFile(filepath.Join(tmpDir, ".codex", "auth.json"), authBytes, 0o600)
	t.Setenv("HOME", tmpDir)
	t.Setenv("AM_HOME", tmpDir)
	t.Setenv("ANTHROPIC_API_KEY", "")

	var mu sync.Mutex
	turnCount := 0

	// Mock upstream OpenAI Codex backend
	codexBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		turnCount++
		currentTurn := turnCount
		mu.Unlock()

		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		// Model must NOT be claude-3-7-sonnet
		model, _ := reqBody["model"].(string)
		if strings.HasPrefix(model, "claude-") {
			http.Error(w, "upstream rejected invalid model: "+model, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		var outputText string
		switch currentTurn {
		case 1:
			// PARALLEL MULTI-SUBAGENTS: 2 subagents dispatched at once
			outputText = `<tool_call>
{"name":"invoke_subagent","arguments":{"role":"Architecture Auditor","prompt":"Audit proxy gateway"}}
</tool_call>
<tool_call>
{"name":"invoke_subagent","arguments":{"role":"Security Auditor","prompt":"Check tunnel security"}}
</tool_call>`
		case 2:
			// PARALLEL MULTI-TOOLS: 3 regular tools executed simultaneously
			outputText = `<tool_call>
{"name":"Read","arguments":{"file_path":"pkg/proxy/server.go"}}
</tool_call>
<tool_call>
{"name":"Read","arguments":{"file_path":"pkg/proxy/tunnel.go"}}
</tool_call>
<tool_call>
{"name":"Bash","arguments":{"command":"go test ./pkg/proxy"}}
</tool_call>`
		case 3:
			// HYBRID MULTI-CALL: Subagent AND tool dispatched in the same turn
			outputText = `<tool_call>
{"name":"invoke_subagent","arguments":{"role":"Refactor Specialist","prompt":"Synthesize patch"}}
</tool_call>
<tool_call>
{"name":"Bash","arguments":{"command":"git diff"}}
</tool_call>`
		case 4:
			// Verification loop
			outputText = `<tool_call>
{"name":"Bash","arguments":{"command":"go test ./pkg/tools"}}
</tool_call>`
		default:
			// Final synthesis turn
			outputText = "Claude Code through Codex proxy completed 5-turn multi-agent, multi-subagent, multi-tool loop successfully."
		}

		evt, _ := json.Marshal(map[string]string{
			"type":  "response.output_text.delta",
			"delta": outputText,
		})
		fmt.Fprintf(w, "data: %s\n\n", evt)
		if flusher != nil {
			flusher.Flush()
		}

		doneEvt, _ := json.Marshal(map[string]string{"type": "response.completed"})
		fmt.Fprintf(w, "data: %s\n\n", doneEvt)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer codexBackend.Close()

	backendURL := codexBackend.URL
	codexAdapter := &provider.CodexCLIAdapter{
		AdapterID:   "codex:01",
		PriorityLvl: 30,
		TargetModel: "",
		HTTPClient: &http.Client{
			Transport: proxyRoundTripperFn(func(req *http.Request) (*http.Response, error) {
				newURL := backendURL + req.URL.Path
				newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
				newReq.Header = req.Header
				return http.DefaultTransport.RoundTrip(newReq)
			}),
		},
	}

	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{codexAdapter})
	pool.SetDirectory([]types.ProviderAdapter{codexAdapter})

	rot := NewRotator("claude")
	life := NewLifecycle()
	mode := &ProxyMode{}
	mode.Set("provider")

	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected call to subscription reverse proxy")
	})

	sw := &swappableHandler{}
	h := newHandler(rot, life, mode, pool, pool, rp, "https://api.anthropic.com", sw, "", func() {})
	sw.Set(h)
	server := httptest.NewServer(sw)
	defer server.Close()

	claudeTools := []map[string]any{
		{"name": "Bash", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
		{"name": "Read", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"file_path": map[string]any{"type": "string"}}}},
		{"name": "invoke_subagent", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"role": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"}}}},
	}

	var messages []map[string]any
	messages = append(messages, map[string]any{"role": "user", "content": "Start multi-agent multi-subagent audit via Codex proxy."})

	for turn := 1; turn <= 5; turn++ {
		reqBody := map[string]any{
			"model":    "claude-3-7-sonnet-20250219",
			"stream":   true,
			"tools":    claudeTools,
			"messages": messages,
		}
		b, _ := json.Marshal(reqBody)

		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("turn %d create request: %v", turn, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Provider", "codex:01")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("turn %d http error: %v", turn, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("turn %d status=%d", turn, resp.StatusCode)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		bodyStr := buf.String()

		switch turn {
		case 1:
			// Verify BOTH subagents arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "Architecture Auditor") || !strings.Contains(bodyStr, "Security Auditor") {
				t.Fatalf("turn 1 expected both subagents through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_sub_1", "name": "invoke_subagent", "input": map[string]any{"role": "Architecture Auditor", "prompt": "Audit proxy gateway"}},
						{"type": "tool_use", "id": "call_sub_2", "name": "invoke_subagent", "input": map[string]any{"role": "Security Auditor", "prompt": "Check tunnel security"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_sub_1", "content": `{"status":"arch_ok"}`},
						{"type": "tool_result", "tool_use_id": "call_sub_2", "content": `{"status":"sec_ok"}`},
					},
				},
			)
		case 2:
			// Verify ALL 3 parallel tools arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "pkg/proxy/server.go") || !strings.Contains(bodyStr, "pkg/proxy/tunnel.go") || !strings.Contains(bodyStr, "go test ./pkg/proxy") {
				t.Fatalf("turn 2 expected all 3 parallel tools through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_t_1", "name": "Read", "input": map[string]any{"file_path": "pkg/proxy/server.go"}},
						{"type": "tool_use", "id": "call_t_2", "name": "Read", "input": map[string]any{"file_path": "pkg/proxy/tunnel.go"}},
						{"type": "tool_use", "id": "call_t_3", "name": "Bash", "input": map[string]any{"command": "go test ./pkg/proxy"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_t_1", "content": "package proxy"},
						{"type": "tool_result", "tool_use_id": "call_t_2", "content": "package proxy // tunnel"},
						{"type": "tool_result", "tool_use_id": "call_t_3", "content": "ok pkg/proxy 0.15s"},
					},
				},
			)
		case 3:
			// Verify HYBRID subagent + tool arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "Refactor Specialist") || !strings.Contains(bodyStr, "git diff") {
				t.Fatalf("turn 3 expected hybrid subagent + tool through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_h_1", "name": "invoke_subagent", "input": map[string]any{"role": "Refactor Specialist", "prompt": "Synthesize patch"}},
						{"type": "tool_use", "id": "call_h_2", "name": "Bash", "input": map[string]any{"command": "git diff"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_h_1", "content": "Refactored cleanly."},
						{"type": "tool_result", "tool_use_id": "call_h_2", "content": "Working tree clean."},
					},
				},
			)
		case 4:
			if !strings.Contains(bodyStr, "go test ./pkg/tools") {
				t.Fatalf("turn 4 expected verification tool through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_v_1", "name": "Bash", "input": map[string]any{"command": "go test ./pkg/tools"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_v_1", "content": "PASS ok pkg/tools 0.3s"},
					},
				},
			)
		case 5:
			if !strings.Contains(bodyStr, "Claude Code through Codex proxy completed 5-turn") {
				t.Fatalf("turn 5 expected final text completion: %s", bodyStr)
			}
		}
	}
}

// ============================================================================
// 2. CLAUDE CODE THROUGH PROXY WITH AGY / GEMINI (gemini:api:01)
// Tests: Multi-Tools, Multi-Agents, Multi-SubAgents, Multi-Loop Agents & Tools
// ============================================================================
func TestProxy_ClaudeCode_AGY_MultiTools_MultiAgent_MultiSubAgent_MultiLoop(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	var mu sync.Mutex
	turnCount := 0

	// Mock upstream Google AI Studio / Gemini endpoint (OpenAI wire format)
	geminiBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		turnCount++
		currentTurn := turnCount
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		sendChunk := func(payload map[string]any) {
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		switch currentTurn {
		case 1:
			// PARALLEL MULTI-SUBAGENTS: 2 subagents via Gemini OpenAI tool_calls
			sendChunk(map[string]any{
				"choices": []map[string]any{
					{
						"delta": map[string]any{
							"tool_calls": []map[string]any{
								{
									"index": 0,
									"id":    "call_sub_1",
									"type":  "function",
									"function": map[string]string{
										"name":      "invoke_subagent",
										"arguments": `{"role":"AGY Architect","prompt":"Audit proxy gateway"}`,
									},
								},
								{
									"index": 1,
									"id":    "call_sub_2",
									"type":  "function",
									"function": map[string]string{
										"name":      "invoke_subagent",
										"arguments": `{"role":"AGY Security","prompt":"Verify auth security"}`,
									},
								},
							},
						},
					},
				},
			})
			sendChunk(map[string]any{"choices": []map[string]any{{"finish_reason": "tool_calls"}}})

		case 2:
			// PARALLEL MULTI-TOOLS: 3 regular tools
			sendChunk(map[string]any{
				"choices": []map[string]any{
					{
						"delta": map[string]any{
							"tool_calls": []map[string]any{
								{
									"index": 0,
									"id":    "call_t_1",
									"type":  "function",
									"function": map[string]string{
										"name":      "Read",
										"arguments": `{"file_path":"pkg/proxy/server.go"}`,
									},
								},
								{
									"index": 1,
									"id":    "call_t_2",
									"type":  "function",
									"function": map[string]string{
										"name":      "Read",
										"arguments": `{"file_path":"pkg/proxy/tunnel.go"}`,
									},
								},
								{
									"index": 2,
									"id":    "call_t_3",
									"type":  "function",
									"function": map[string]string{
										"name":      "Bash",
										"arguments": `{"command":"go test ./pkg/proxy"}`,
									},
								},
							},
						},
					},
				},
			})
			sendChunk(map[string]any{"choices": []map[string]any{{"finish_reason": "tool_calls"}}})

		case 3:
			// HYBRID MULTI-CALL: Subagent AND tool in the same turn
			sendChunk(map[string]any{
				"choices": []map[string]any{
					{
						"delta": map[string]any{
							"tool_calls": []map[string]any{
								{
									"index": 0,
									"id":    "call_h_1",
									"type":  "function",
									"function": map[string]string{
										"name":      "invoke_subagent",
										"arguments": `{"role":"AGY Refactor","prompt":"Synthesize patch"}`,
									},
								},
								{
									"index": 1,
									"id":    "call_h_2",
									"type":  "function",
									"function": map[string]string{
										"name":      "Bash",
										"arguments": `{"command":"git diff"}`,
									},
								},
							},
						},
					},
				},
			})
			sendChunk(map[string]any{"choices": []map[string]any{{"finish_reason": "tool_calls"}}})

		case 4:
			// Verification loop
			sendChunk(map[string]any{
				"choices": []map[string]any{
					{
						"delta": map[string]any{
							"tool_calls": []map[string]any{
								{
									"index": 0,
									"id":    "call_v_1",
									"type":  "function",
									"function": map[string]string{
										"name":      "Bash",
										"arguments": `{"command":"go test ./pkg/tools"}`,
									},
								},
							},
						},
					},
				},
			})
			sendChunk(map[string]any{"choices": []map[string]any{{"finish_reason": "tool_calls"}}})

		default:
			// Final synthesis turn
			sendChunk(map[string]any{
				"choices": []map[string]any{
					{
						"delta": map[string]any{
							"content": "Claude Code through AGY proxy completed 5-turn multi-agent, multi-subagent, multi-tool loop successfully.",
						},
					},
				},
			})
			sendChunk(map[string]any{"choices": []map[string]any{{"finish_reason": "stop"}}})
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer geminiBackend.Close()

	agyAdapter := &provider.OpenAICompatibleAdapter{
		AdapterID:   "gemini:api:01",
		PriorityLvl: 2,
		BaseURL:     geminiBackend.URL,
		APIKey:      "mock-key",
		TargetModel: "gemini-3.8-flash",
		HTTPClient:  geminiBackend.Client(),
	}

	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{agyAdapter})
	pool.SetDirectory([]types.ProviderAdapter{agyAdapter})

	rot := NewRotator("claude")
	life := NewLifecycle()
	mode := &ProxyMode{}
	mode.Set("provider")

	rp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected call to subscription reverse proxy")
	})

	sw := &swappableHandler{}
	h := newHandler(rot, life, mode, pool, pool, rp, "https://api.anthropic.com", sw, "", func() {})
	sw.Set(h)
	server := httptest.NewServer(sw)
	defer server.Close()

	claudeTools := []map[string]any{
		{"name": "Bash", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}},
		{"name": "Read", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"file_path": map[string]any{"type": "string"}}}},
		{"name": "invoke_subagent", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"role": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"}}}},
	}

	var messages []map[string]any
	messages = append(messages, map[string]any{"role": "user", "content": "Start multi-agent audit via AGY proxy."})

	for turn := 1; turn <= 5; turn++ {
		reqBody := map[string]any{
			"model":    "claude-3-7-sonnet-20250219",
			"stream":   true,
			"tools":    claudeTools,
			"messages": messages,
		}
		b, _ := json.Marshal(reqBody)

		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("turn %d create request: %v", turn, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Provider", "gemini:api:01")

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("turn %d http error: %v", turn, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("turn %d status=%d", turn, resp.StatusCode)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		bodyStr := buf.String()

		switch turn {
		case 1:
			// Verify BOTH subagents arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "AGY Architect") || !strings.Contains(bodyStr, "AGY Security") {
				t.Fatalf("turn 1 expected both subagents through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_sub_1", "name": "invoke_subagent", "input": map[string]any{"role": "AGY Architect", "prompt": "Audit proxy gateway"}},
						{"type": "tool_use", "id": "call_sub_2", "name": "invoke_subagent", "input": map[string]any{"role": "AGY Security", "prompt": "Verify auth security"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_sub_1", "content": `{"status":"arch_ok"}`},
						{"type": "tool_result", "tool_use_id": "call_sub_2", "content": `{"status":"sec_ok"}`},
					},
				},
			)
		case 2:
			// Verify ALL 3 parallel tools arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "pkg/proxy/server.go") || !strings.Contains(bodyStr, "pkg/proxy/tunnel.go") || !strings.Contains(bodyStr, "go test ./pkg/proxy") {
				t.Fatalf("turn 2 expected all 3 parallel tools through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_t_1", "name": "Read", "input": map[string]any{"file_path": "pkg/proxy/server.go"}},
						{"type": "tool_use", "id": "call_t_2", "name": "Read", "input": map[string]any{"file_path": "pkg/proxy/tunnel.go"}},
						{"type": "tool_use", "id": "call_t_3", "name": "Bash", "input": map[string]any{"command": "go test ./pkg/proxy"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_t_1", "content": "package proxy"},
						{"type": "tool_result", "tool_use_id": "call_t_2", "content": "package proxy // tunnel"},
						{"type": "tool_result", "tool_use_id": "call_t_3", "content": "ok pkg/proxy 0.15s"},
					},
				},
			)
		case 3:
			// Verify HYBRID subagent + tool arrived through the proxy in the same turn
			if !strings.Contains(bodyStr, "AGY Refactor") || !strings.Contains(bodyStr, "git diff") {
				t.Fatalf("turn 3 expected hybrid subagent + tool through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_h_1", "name": "invoke_subagent", "input": map[string]any{"role": "AGY Refactor", "prompt": "Synthesize patch"}},
						{"type": "tool_use", "id": "call_h_2", "name": "Bash", "input": map[string]any{"command": "git diff"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_h_1", "content": "Refactored cleanly."},
						{"type": "tool_result", "tool_use_id": "call_h_2", "content": "Working tree clean."},
					},
				},
			)
		case 4:
			if !strings.Contains(bodyStr, "go test ./pkg/tools") {
				t.Fatalf("turn 4 expected verification tool through proxy: %s", bodyStr)
			}
			messages = append(messages,
				map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "tool_use", "id": "call_v_1", "name": "Bash", "input": map[string]any{"command": "go test ./pkg/tools"}},
					},
				},
				map[string]any{
					"role": "user",
					"content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "call_v_1", "content": "PASS ok pkg/tools 0.3s"},
					},
				},
			)
		case 5:
			if !strings.Contains(bodyStr, "Claude Code through AGY proxy completed 5-turn") {
				t.Fatalf("turn 5 expected final text completion: %s", bodyStr)
			}
		}
	}
}
