package bridge_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

func TestToChatRequest_SystemAndMessages(t *testing.T) {
	rawJSON := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"system": "You are a senior golang engineer.",
		"messages": [
			{"role": "user", "content": "Explain channels in Go"},
			{"role": "assistant", "content": "Channels are concurrency primitives"},
			{"role": "user", "content": [{"type": "text", "text": "Give an example"}]}
		],
		"stream": true,
		"max_tokens": 1024
	}`)

	req, err := bridge.ToChatRequest(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model claude-3-5-sonnet-20241022, got %s", req.Model)
	}
	if !req.Stream {
		t.Errorf("expected stream=true")
	}
	if !req.FullContext {
		t.Errorf("expected FullContext=true for Claude Code bridge")
	}
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages (1 system + 3 turns), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are a senior golang engineer." {
		t.Errorf("unexpected system message: %+v", req.Messages[0])
	}
	if req.Messages[3].Role != "user" || req.Messages[3].Content != "Give an example" {
		t.Errorf("unexpected block message: %+v", req.Messages[3])
	}
}

func TestToChatRequest_ExpandsToolHistory(t *testing.T) {
	raw := []byte(`{
		"model":"claude-sonnet-4-20250514",
		"tools":[{"name":"Bash","description":"shell","input_schema":{"type":"object","properties":{}}}],
		"messages":[{
			"role":"user",
			"content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"package main"},
				{"type":"text","text":"fix the panic"}
			]
		}],
		"stream":false
	}`)
	req, err := bridge.ToChatRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Bash" {
		t.Fatalf("tools=%+v", req.Tools)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("msgs=%d want 2 (tool + user text), got %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "tool" || req.Messages[0].ToolCallID != "toolu_1" || req.Messages[0].Content != "package main" {
		t.Fatalf("tool msg=%+v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "fix the panic" {
		t.Fatalf("user msg=%+v", req.Messages[1])
	}
}

func TestClaudeBridge_StreamingToolCallsNoMarkupLeak(t *testing.T) {
	adapter := &toolCallAdapter{
		id:   "claude-stream-leak",
		text: `I will check the directory: <tool_call>{"name":"Bash","arguments":{"command":"ls"}}</tool_call>`,
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{adapter})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"tools": [{"name":"Bash","description":"shell","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}],
		"messages": [{"role":"user","content":"list files"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(rec, req, pool, body); err != nil {
		t.Fatal(err)
	}
	out := rec.Body.String()
	// Must contain tool_use block
	if !strings.Contains(out, `"type":"tool_use"`) || !strings.Contains(out, `"name":"Bash"`) {
		t.Fatalf("expected tool_use block in output, got: %s", out)
	}
	// Must NOT contain <tool_call> markup in text_delta
	if strings.Contains(out, "<tool_call") {
		t.Fatalf("tool markup leaked into text_delta stream: %s", out)
	}
}
