package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestIsCodexCompatibleModel(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-3-7-sonnet-20250219", false},
		{"claude-3-5-sonnet-latest", false},
		{"gemini-2.5-pro", false},
		{"deepseek-chat", false},
		{"kimi-k1.5", false},
		{"grok-2", false},
		{"gpt-5.6-terra", true},
		{"gpt-4o", false},
		{"o1-preview", true},
		{"o3-mini", true},
		{"o4", true},
		{"codex-mini", true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := isCodexCompatibleModel(tc.model)
			if got != tc.want {
				t.Errorf("isCodexCompatibleModel(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestCodexCLIAdapter_SupportsTools(t *testing.T) {
	a := &CodexCLIAdapter{AdapterID: "codex:01"}
	if !a.SupportsTools() {
		t.Fatal("Codex proxy must advertise native tools")
	}
}

func writeCodexTestAuth(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	_ = os.MkdirAll(filepath.Join(tmpDir, ".codex"), 0o755)
	authDoc := map[string]any{
		"tokens": map[string]any{
			"access_token":  "mock-jwt.eyJleHAiOjk5OTk5OTk5OTl9.signature",
			"refresh_token": "mock-refresh",
			"account_id":    "acc-123",
		},
	}
	authBytes, _ := json.Marshal(authDoc)
	if err := os.WriteFile(filepath.Join(tmpDir, ".codex", "auth.json"), authBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newCodexTestAdapter(t *testing.T, handler http.HandlerFunc) (*CodexCLIAdapter, *map[string]any, *http.Header) {
	t.Helper()
	writeCodexTestAuth(t)
	var receivedBody map[string]any
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	adapter := &CodexCLIAdapter{
		AdapterID:   "codex:01",
		PriorityLvl: 30,
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				newURL := server.URL + req.URL.Path
				newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
				newReq.Header = req.Header
				return http.DefaultTransport.RoundTrip(newReq)
			}),
		},
	}
	return adapter, &receivedBody, &receivedHeaders
}

func TestCodexCLIAdapter_SendMessageStream_NativeClaudeTools(t *testing.T) {
	adapter, receivedBody, receivedHeaders := newCodexTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		item, _ := json.Marshal(map[string]any{
			"type": "response.output_item.added",
			"item": map[string]any{
				"id":        "fc_1",
				"type":      "function_call",
				"name":      "Bash",
				"call_id":   "toolu_bash",
				"arguments": "",
			},
		})
		fmt.Fprintf(w, "data: %s\n\n", item)
		delta, _ := json.Marshal(map[string]any{
			"type":    "response.function_call_arguments.delta",
			"call_id": "toolu_bash",
			"delta":   `{"command":"ls -la"}`,
		})
		fmt.Fprintf(w, "data: %s\n\n", delta)
		done, _ := json.Marshal(map[string]any{
			"type":      "response.function_call_arguments.done",
			"call_id":   "toolu_bash",
			"name":      "Bash",
			"arguments": `{"command":"ls -la"}`,
		})
		fmt.Fprintf(w, "data: %s\n\n", done)
		completed, _ := json.Marshal(map[string]any{"type": "response.completed"})
		fmt.Fprintf(w, "data: %s\n\n", completed)
		if flusher != nil {
			flusher.Flush()
		}
	})

	req := &types.ChatRequest{
		Model:       "claude-3-7-sonnet-20250219",
		FullContext: true,
		Tools: []types.ToolDef{
			{
				Name:        "Bash",
				Description: "Run command",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
			},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are Claude Code, Anthropic's official CLI."},
			{Role: "user", Content: "List directory"},
		},
	}

	ch, err := adapter.SendMessageStream(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}
	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	body := *receivedBody
	receivedModel, _ := body["model"].(string)
	if receivedModel == "claude-3-7-sonnet-20250219" {
		t.Errorf("model was erroneously set to Claude model: %s", receivedModel)
	}
	if receivedModel != codexDefaultModel {
		t.Errorf("expected default codex model %s, got: %s", codexDefaultModel, receivedModel)
	}
	if (*receivedHeaders).Get("Authorization") != "Bearer mock-jwt.eyJleHAiOjk5OTk5OTk5OTl9.signature" {
		t.Errorf("missing Authorization: %v", (*receivedHeaders).Get("Authorization"))
	}
	if (*receivedHeaders).Get("chatgpt-account-id") != "acc-123" {
		t.Errorf("missing account-id: %v", (*receivedHeaders).Get("chatgpt-account-id"))
	}

	toolsRaw, _ := json.Marshal(body["tools"])
	if !strings.Contains(string(toolsRaw), `"name":"Bash"`) {
		t.Fatalf("expected native tools[], got %s", toolsRaw)
	}
	if strings.Contains(string(toolsRaw), "input_schema") {
		t.Fatalf("Claude input_schema leaked into Codex payload: %s", toolsRaw)
	}
	if strings.Contains(fmt.Sprint(body["input"]), "<tool_call>") {
		t.Fatalf("tools were flattened to webloop prompt: %+v", body["input"])
	}
	inputs, _ := body["input"].([]any)
	if len(inputs) != 1 {
		t.Fatalf("expected 1 user input item (harness dropped), got %+v", inputs)
	}

	found := false
	for _, chunk := range chunks {
		for _, tc := range chunk.ToolCalls {
			if tc.Name == "Bash" && strings.Contains(tc.Arguments, "ls -la") && tc.ID == "toolu_bash" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected native ToolCall Bash, got %+v", chunks)
	}
}

func TestCodexCLIAdapter_SendMessageStream_WebLoopFallback(t *testing.T) {
	adapter, receivedBody, _ := newCodexTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		toolOutput := `<tool_call>
{"name":"Bash","arguments":{"command":"ls -la"}}
</tool_call>`
		evt, _ := json.Marshal(map[string]string{
			"type":  "response.output_text.delta",
			"delta": toolOutput,
		})
		fmt.Fprintf(w, "data: %s\n\n", evt)
		doneEvt, _ := json.Marshal(map[string]string{"type": "response.completed"})
		fmt.Fprintf(w, "data: %s\n\n", doneEvt)
	})

	req := &types.ChatRequest{
		Model: "gpt-5.6-terra",
		Tools: []types.ToolDef{{Name: "Bash", Description: "Run command"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "List directory"},
		},
	}
	ch, err := adapter.SendMessageStream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []types.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	toolsRaw, _ := json.Marshal((*receivedBody)["tools"])
	if !strings.Contains(string(toolsRaw), `"name":"Bash"`) {
		t.Fatalf("fallback still sends native tools[], got %s", toolsRaw)
	}
	found := false
	for _, chunk := range chunks {
		for _, tc := range chunk.ToolCalls {
			if tc.Name == "Bash" && strings.Contains(tc.Arguments, "ls -la") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected webloop-parsed Bash, got %+v", chunks)
	}
}

func TestCodexCLIAdapter_AGYToolResultRoundTrip(t *testing.T) {
	adapter, receivedBody, _ := newCodexTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		text, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": "done"})
		fmt.Fprintf(w, "data: %s\n\n", text)
		done, _ := json.Marshal(map[string]string{"type": "response.completed"})
		fmt.Fprintf(w, "data: %s\n\n", done)
	})

	req := &types.ChatRequest{
		Model: "gemini-2.5-pro",
		Tools: []types.ToolDef{{
			Name:        "run_command",
			Description: "shell",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"CommandLine":{"type":"string"}}}`),
		}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "status"},
			{Role: "assistant", ToolCalls: []types.ToolCall{
				{ID: "gemini_call_1", Name: "run_command", Arguments: `{"CommandLine":"git status"}`},
			}},
			{Role: "tool", ToolCallID: "gemini_call_1", Name: "run_command", Content: "clean"},
		},
	}
	ch, err := adapter.SendMessageStream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	body := *receivedBody
	toolsRaw, _ := json.Marshal(body["tools"])
	if !strings.Contains(string(toolsRaw), `"name":"run_command"`) {
		t.Fatalf("AGY tools not converted: %s", toolsRaw)
	}
	inputRaw, _ := json.Marshal(body["input"])
	if !strings.Contains(string(inputRaw), `"function_call"`) || !strings.Contains(string(inputRaw), `"function_call_output"`) {
		t.Fatalf("AGY tool result not mapped to Responses input: %s", inputRaw)
	}
	if strings.Contains(string(inputRaw), "functionDeclarations") {
		t.Fatalf("Gemini wire leaked: %s", inputRaw)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
