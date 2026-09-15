package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestClaudeAdapter_SendMessageStream_ToolCalls(t *testing.T) {
	origURL := claudeMessagesURL
	defer func() { claudeMessagesURL = origURL }()

	var receivedBody map[string]any
	var receivedAuth string
	var receivedVersion string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("x-api-key")
		receivedVersion = r.Header.Get("anthropic-version")

		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// 1. message_start
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\"}}\n\n")

		// 2. content_block_start text
		fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		// delta text
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"I will check directory.\"}}\n\n")
		// stop text
		fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")

		// 3. content_block_start tool_use
		fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_test_123\",\"name\":\"exec_command\"}}\n\n")
		// delta input_json
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"cmd\\\":\\\"ls -la\\\"}\"}}\n\n")
		// stop tool_use
		fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n")

		// 4. message_delta stop_reason
		fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	claudeMessagesURL = srv.URL

	adapter := &ClaudeAdapter{
		AdapterID:   "claude:api:01",
		PriorityLvl: 1,
		APIKey:      "sk-ant-test-key",
		HTTPClient:  srv.Client(),
	}

	req := &types.ChatRequest{
		Model: "claude-3-7-sonnet-20250219",
		Tools: []types.ToolDef{
			{
				Name:        "exec_command",
				Description: "Run a shell command",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
			},
		},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "List files"},
		},
	}

	ch, err := adapter.SendMessageStream(context.Background(), req)
	if err != nil {
		t.Fatalf("SendMessageStream failed: %v", err)
	}

	var chunks []types.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if receivedAuth != "sk-ant-test-key" {
		t.Errorf("expected x-api-key 'sk-ant-test-key', got %s", receivedAuth)
	}
	if receivedVersion != "2023-06-01" {
		t.Errorf("expected anthropic-version '2023-06-01', got %s", receivedVersion)
	}

	toolsRaw, _ := json.Marshal(receivedBody["tools"])
	if !strings.Contains(string(toolsRaw), `"name":"exec_command"`) {
		t.Fatalf("missing tools in request: %s", toolsRaw)
	}

	var fullContent strings.Builder
	var toolCalls []types.ToolCall
	for _, c := range chunks {
		fullContent.WriteString(c.Content)
		if len(c.ToolCalls) > 0 {
			toolCalls = append(toolCalls, c.ToolCalls...)
		}
	}

	if !strings.Contains(fullContent.String(), "I will check directory.") {
		t.Errorf("expected content 'I will check directory.', got: %s", fullContent.String())
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(toolCalls), toolCalls)
	}
	if toolCalls[0].ID != "toolu_test_123" || toolCalls[0].Name != "exec_command" {
		t.Errorf("unexpected tool call: %+v", toolCalls[0])
	}
	if !strings.Contains(toolCalls[0].Arguments, "ls -la") {
		t.Errorf("unexpected arguments: %s", toolCalls[0].Arguments)
	}
}
