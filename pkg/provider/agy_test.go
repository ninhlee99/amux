package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestNormalizeAGYModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", DefaultAGYModel},
		{"gemini-3.8-flash-high", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash-low", "gemini-3.8-flash-low"},
		{"gemini-3.8-flash", "gemini-3.8-flash-medium"},
		{"claude-3-7-sonnet", DefaultAGYModel},
		{"gemini-3.1-pro", "gemini-3.1-pro-high"},
		{"gemini-2.5-pro", "gemini-3.1-pro-high"},
		{"gpt-4o", "gpt-oss-120b-medium"},
	}

	for _, tt := range tests {
		got := normalizeAGYModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeAGYModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAntigravityAdapter_CLIIntegration(t *testing.T) {
	bin := findAGYBinary()
	if bin == "" {
		t.Skip("agy CLI not found in environment, skipping live test")
	}

	adapter := &AntigravityAdapter{
		AdapterID:   "agy:test",
		PriorityLvl: 1,
		TargetModel: "gemini-3.8-flash-low",
	}

	req := &types.ChatRequest{
		Model: "gemini-3.8-flash",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Reply with the exact word 'PONG'."},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	ch, err := adapter.SendMessageStream(ctx, req)
	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("Stream chunk returned error: %v", chunk.Error)
		}
		sb.WriteString(chunk.Content)
	}

	resp := strings.TrimSpace(sb.String())
	if !strings.Contains(strings.ToUpper(resp), "PONG") {
		t.Fatalf("expected PONG in response, got: %q", resp)
	}
}

func TestAntigravityAdapter_PureHTTPIntegration(t *testing.T) {
	if !AGYAuthAvailable() {
		t.Skip("No AGY credentials available, skipping pure HTTP test")
	}

	adapter := &AntigravityAdapter{
		AdapterID:   "antigravity:http_test",
		PriorityLvl: 1,
		TargetModel: "gemini-3.8-flash-low",
	}

	req := &types.ChatRequest{
		Model: "gemini-3.8-flash",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Reply with the single word PONG"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ch, err := adapter.sendHTTPStream(ctx, req)
	if err != nil {
		t.Fatalf("sendHTTPStream failed: %v", err)
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("Stream chunk error: %v", chunk.Error)
		}
		sb.WriteString(chunk.Content)
	}

	got := strings.TrimSpace(sb.String())
	if !strings.Contains(strings.ToUpper(got), "PONG") {
		t.Fatalf("expected PONG in response, got: %q", got)
	}
}
func TestBuildAGYRequestBody_ClaudeToolsFormatting(t *testing.T) {
	req := &types.ChatRequest{
		Model: "claude-3-7-sonnet-20250219",
		Tools: []types.ToolDef{
			{
				Name:        "Bash",
				Description: "Run shell command",
				InputSchema: []byte(`{"type":"object","properties":{"command":{"type":"string"}}}`),
			},
			{
				Name:        "Read",
				Description: "Read file content",
				InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
		Messages: []types.ChatMessage{
			{
				Role:    "user",
				Content: "List files and read readme",
			},
			{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{
						ID:               "call_bash_1",
						Name:             "Bash",
						Arguments:        `{"command":"ls -la"}`,
						ThoughtSignature: "sig_bash_123",
					},
					{
						ID:               "call_read_2",
						Name:             "Read",
						Arguments:        `{"path":"README.md"}`,
						ThoughtSignature: "sig_read_456",
					},
				},
			},
			// Claude sends role="tool" with ToolCallID but no Name
			{
				Role:       "tool",
				ToolCallID: "call_bash_1",
				Content:    "README.md\nmain.go",
			},
			{
				Role:       "tool",
				ToolCallID: "call_read_2",
				Content:    "# Project Readme",
			},
		},
	}

	body := buildAGYRequestBody("aicode-consumers", "gemini-3.8-flash-medium", req)

	// 1. Verify tools are converted to Gemini function declarations
	if len(body.Request.Tools) != 1 || len(body.Request.Tools[0].FunctionDeclarations) != 2 {
		t.Fatalf("expected 2 function declarations, got: %+v", body.Request.Tools)
	}

	// 2. Verify contents structure:
	// Turn 0: user text
	// Turn 1: model tool calls (with thought signatures)
	// Turn 2: user merged tool responses (alternating turns enforced)
	contents := body.Request.Contents
	if len(contents) != 3 {
		t.Fatalf("expected 3 alternating turns, got %d turns: %+v", len(contents), contents)
	}

	if contents[0].Role != "user" || len(contents[0].Parts) != 1 || contents[0].Parts[0].Text == "" {
		t.Errorf("turn 0 should be user text: %+v", contents[0])
	}

	if contents[1].Role != "model" || len(contents[1].Parts) != 2 {
		t.Fatalf("turn 1 should have 2 functionCall parts: %+v", contents[1])
	}
	if contents[1].Parts[0].FunctionCall.Name != "Bash" || contents[1].Parts[0].FunctionCall.ThoughtSignature != "sig_bash_123" {
		t.Errorf("turn 1 part 0 incorrect: %+v", contents[1].Parts[0])
	}
	if contents[1].Parts[1].FunctionCall.Name != "Read" || contents[1].Parts[1].FunctionCall.ThoughtSignature != "sig_read_456" {
		t.Errorf("turn 1 part 1 incorrect: %+v", contents[1].Parts[1])
	}

	// Turn 2 must have role "user" and contain 2 FunctionResponses with resolved Names
	if contents[2].Role != "user" || len(contents[2].Parts) != 2 {
		t.Fatalf("turn 2 should merge parallel tool responses into 2 parts: %+v", contents[2])
	}
	if contents[2].Parts[0].FunctionResponse == nil || contents[2].Parts[0].FunctionResponse.Name != "Bash" {
		t.Errorf("turn 2 part 0 expected Bash functionResponse, got: %+v", contents[2].Parts[0])
	}
	if contents[2].Parts[1].FunctionResponse == nil || contents[2].Parts[1].FunctionResponse.Name != "Read" {
		t.Errorf("turn 2 part 1 expected Read functionResponse, got: %+v", contents[2].Parts[1])
	}
	// Verify no stray Text part is present
	for idx, p := range contents[2].Parts {
		if p.Text != "" {
			t.Errorf("turn 2 part %d should not have text, got: %q", idx, p.Text)
		}
	}
}
