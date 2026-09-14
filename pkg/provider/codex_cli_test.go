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

func TestCodexCLIAdapter_SendMessageStream_ClaudeModelAndTools(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authDoc := map[string]any{
		"tokens": map[string]any{
			"access_token":  "mock-jwt.eyJleHAiOjk5OTk5OTk5OTl9.signature",
			"refresh_token": "mock-refresh",
			"account_id":    "acc-123",
		},
	}
	authBytes, _ := json.Marshal(authDoc)
	_ = os.WriteFile(authPath, authBytes, 0o600)
	t.Setenv("HOME", tmpDir)
	_ = os.MkdirAll(filepath.Join(tmpDir, ".codex"), 0o755)
	_ = os.WriteFile(filepath.Join(tmpDir, ".codex", "auth.json"), authBytes, 0o600)

	var receivedBody map[string]any
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		// Stream simulated tool call
		toolOutput := `<tool_call>
{"name":"Bash","arguments":{"command":"ls -la"}}
</tool_call>`
		evt, _ := json.Marshal(map[string]string{
			"type":  "response.output_text.delta",
			"delta": toolOutput,
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
	defer server.Close()

	adapter := &CodexCLIAdapter{
		AdapterID:   "codex:01",
		PriorityLvl: 30,
		TargetModel: "",
		HTTPClient:  server.Client(),
	}

	// Override HTTP request URL for test by using custom transport
	serverURL := server.URL
	adapter.HTTPClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			newURL := serverURL + req.URL.Path
			newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
			newReq.Header = req.Header
			return http.DefaultTransport.RoundTrip(newReq)
		}),
	}

	req := &types.ChatRequest{
		Model:       "claude-3-7-sonnet-20250219", // Claude model from Claude Code
		FullContext: true,
		Tools: []types.ToolDef{
			{Name: "Bash", Description: "Run command"},
		},
		Messages: []types.ChatMessage{
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

	// 1. Verify model was NOT overridden with Claude model
	receivedModel, _ := receivedBody["model"].(string)
	if receivedModel == "claude-3-7-sonnet-20250219" {
		t.Errorf("model was erroneously set to Claude model: %s", receivedModel)
	}
	if receivedModel != codexDefaultModel {
		t.Errorf("expected default codex model %s, got: %s", codexDefaultModel, receivedModel)
	}

	// 2. Verify headers
	if receivedHeaders.Get("Authorization") != "Bearer mock-jwt.eyJleHAiOjk5OTk5OTk5OTl9.signature" {
		t.Errorf("missing or invalid Authorization header: %v", receivedHeaders.Get("Authorization"))
	}
	if receivedHeaders.Get("chatgpt-account-id") != "acc-123" {
		t.Errorf("missing or invalid account-id header: %v", receivedHeaders.Get("chatgpt-account-id"))
	}

	// 3. Verify input contains WebPreamble with tool definitions
	inputs, _ := receivedBody["input"].([]any)
	if len(inputs) == 0 {
		t.Fatalf("expected non-empty input array")
	}
	firstMsg, _ := inputs[0].(map[string]any)
	content, _ := firstMsg["content"].(string)
	if !strings.Contains(content, "Bash") || !strings.Contains(content, "<tool_call>") {
		t.Errorf("input does not contain tool definitions/preamble: %s", content)
	}

	// 4. Verify MaybeWrapWebStream extracted the ToolCall
	foundToolCall := false
	for _, chunk := range chunks {
		for _, tc := range chunk.ToolCalls {
			if tc.Name == "Bash" && strings.Contains(tc.Arguments, "ls -la") {
				foundToolCall = true
			}
		}
	}
	if !foundToolCall {
		t.Errorf("expected parsed ToolCall for Bash, got chunks: %+v", chunks)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
