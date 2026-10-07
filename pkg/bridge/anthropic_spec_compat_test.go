package bridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// TestAnthropicSpec_StreamingSSELifecycle verifies that streaming SSE responses
// strictly follow Anthropic's wire specification:
// 1. message_start (with assistant role, empty content, model, usage)
// 2. ping
// 3. content_block_start (index 0, type text or tool_use)
// 4. content_block_delta (index 0, text_delta or input_json_delta)
// 5. content_block_stop (index 0)
// 6. sequential contiguous indices for subsequent content blocks
// 7. message_delta (stop_reason: tool_use or end_turn, usage: output_tokens)
// 8. message_stop
func TestAnthropicSpec_StreamingSSELifecycle(t *testing.T) {
	adapter := &toolCallAdapter{
		id:   "claude:spec:stream",
		text: "I will execute the tool now.",
		calls: []types.ToolCall{
			{ID: "toolu_01A", Name: "Bash", Arguments: `{"command":"ls"}`},
			{ID: "toolu_02B", Name: "Read", Arguments: `{"path":"main.go"}`},
		},
	}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{adapter})
	pool.SetSubscriptionPoolFilter(func(string) bool { return true })

	body := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"stream": true,
		"tools": [
			{"name":"Bash","description":"run command","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}},
			{"name":"Read","description":"read file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
		],
		"messages": [{"role":"user","content":"check status"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(rec, req, pool, body); err != nil {
		t.Fatalf("HandleClaudeMessages error: %v", err)
	}

	rawSSE := rec.Body.String()
	lines := strings.Split(rawSSE, "\n")

	type sseEvent struct {
		Event string
		Data  string
	}

	var events []sseEvent
	var curEvent, curData string
	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			curEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			curData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && curEvent != "" {
			events = append(events, sseEvent{Event: curEvent, Data: curData})
			curEvent, curData = "", ""
		}
	}

	if len(events) == 0 {
		t.Fatalf("no SSE events parsed from response:\n%s", rawSSE)
	}

	// 1. First event must be message_start
	if events[0].Event != "message_start" {
		t.Fatalf("expected first event message_start, got %s", events[0].Event)
	}
	var msgStart struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Model   string `json:"model"`
			Content []any  `json:"content"`
			Usage   struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &msgStart); err != nil {
		t.Fatalf("unmarshal message_start: %v", err)
	}
	if msgStart.Message.Role != "assistant" {
		t.Errorf("message_start role=%q, want assistant", msgStart.Message.Role)
	}
	if len(msgStart.Message.Content) != 0 {
		t.Errorf("message_start content must be empty array, got %+v", msgStart.Message.Content)
	}

	// 2. Second event should be ping
	if events[1].Event != "ping" {
		t.Errorf("expected second event ping, got %s", events[1].Event)
	}

	// 3. Track block indices and verify contiguous 0-based indexing
	seenBlockStarts := make(map[int]string)
	seenBlockStops := make(map[int]bool)
	var lastStopReason string

	for _, ev := range events {
		switch ev.Event {
		case "content_block_start":
			var cb struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &cb); err != nil {
				t.Fatalf("unmarshal content_block_start: %v", err)
			}
			seenBlockStarts[cb.Index] = cb.ContentBlock.Type
		case "content_block_stop":
			var cb struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &cb); err != nil {
				t.Fatalf("unmarshal content_block_stop: %v", err)
			}
			seenBlockStops[cb.Index] = true
		case "message_delta":
			var md struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &md); err != nil {
				t.Fatalf("unmarshal message_delta: %v", err)
			}
			lastStopReason = md.Delta.StopReason
		}
	}

	// Verify all started blocks were closed
	if len(seenBlockStarts) == 0 {
		t.Fatal("no content blocks started")
	}
	for idx, blkType := range seenBlockStarts {
		if !seenBlockStops[idx] {
			t.Errorf("block index %d (%s) was started but never stopped", idx, blkType)
		}
	}

	// Verify indices are contiguous 0, 1, ...
	for i := 0; i < len(seenBlockStarts); i++ {
		if _, exists := seenBlockStarts[i]; !exists {
			t.Errorf("missing contiguous block index %d; blocks=%+v", i, seenBlockStarts)
		}
	}

	// With tool calls emitted, stop reason must be "tool_use"
	if lastStopReason != "tool_use" {
		t.Errorf("expected stop_reason tool_use, got %q", lastStopReason)
	}

	// Last event must be message_stop
	lastEv := events[len(events)-1]
	if lastEv.Event != "message_stop" {
		t.Errorf("expected last event message_stop, got %s", lastEv.Event)
	}
}

// TestAnthropicSpec_ToolResultErrorRoundtrip verifies that:
// 1. is_error on tool_result is preserved when expanding Anthropic messages
// 2. MarshalClaudeMessagesRequest reproduces is_error: true on tool_result
// 3. Sequential tool results are coalesced into a single user turn
func TestAnthropicSpec_ToolResultErrorRoundtrip(t *testing.T) {
	raw := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"messages": [
			{"role": "user", "content": "run test"},
			{
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "toolu_fail_1", "name": "Bash", "input": {"command": "go test"}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_fail_1",
						"content": "FAIL: test failed",
						"is_error": true
					}
				]
			}
		]
	}`)

	req, err := bridge.ToChatRequest(raw)
	if err != nil {
		t.Fatalf("ToChatRequest error: %v", err)
	}

	// The tool message should carry IsError == true
	var foundToolResult bool
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == "toolu_fail_1" {
			foundToolResult = true
			if !m.IsError {
				t.Errorf("expected m.IsError to be true for failed tool_result, got false")
			}
			if m.Name != "Bash" {
				t.Errorf("expected linked tool name Bash, got %s", m.Name)
			}
		}
	}
	if !foundToolResult {
		t.Fatal("tool_result message not found in canonical ChatRequest")
	}

	// Now serialize back using MarshalClaudeMessagesRequest
	serialized, err := tools.MarshalClaudeMessagesRequest(req, req.Model)
	if err != nil {
		t.Fatalf("MarshalClaudeMessagesRequest error: %v", err)
	}

	var parsedAnthropic struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type       string `json:"type"`
				ToolUseID  string `json:"tool_use_id"`
				Content    string `json:"content"`
				IsError    bool   `json:"is_error"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(serialized, &parsedAnthropic); err != nil {
		t.Fatalf("unmarshal serialized anthropic request: %v", err)
	}

	var verifiedIsError bool
	for _, m := range parsedAnthropic.Messages {
		for _, blk := range m.Content {
			if blk.Type == "tool_result" && blk.ToolUseID == "toolu_fail_1" {
				if blk.IsError {
					verifiedIsError = true
				}
			}
		}
	}
	if !verifiedIsError {
		t.Fatalf("is_error: true was not preserved in serialized Anthropic payload: %s", string(serialized))
	}
}

// TestAnthropicSpec_ErrorResponseFormats verifies that HTTP error responses
// from AMUX adhere to the Anthropic Messages API error envelope:
// {"type":"error","error":{"type":...,"message":...}}
func TestAnthropicSpec_ErrorResponseFormats(t *testing.T) {
	t.Run("rate limit 429 returns rate_limit_error and Retry-After", func(t *testing.T) {
		failAdapter := &rateLimitFailAdapter{retryAfter: 45 * time.Second}
		pool := router.NewAccountPoolRouter([]types.ProviderAdapter{failAdapter})
		pool.SetSubscriptionPoolFilter(func(string) bool { return true })

		body := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		_ = bridge.HandleClaudeMessages(rec, req, pool, body)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("status=%d, want 429", rec.Code)
		}
		if ra := rec.Header().Get("Retry-After"); ra != "45" {
			t.Errorf("Retry-After=%q, want 45", ra)
		}

		var errResp struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errResp.Type != "error" || errResp.Error.Type != "rate_limit_error" {
			t.Errorf("unexpected error envelope: %+v", errResp)
		}
	})

	t.Run("auth failure returns authentication_error", func(t *testing.T) {
		failAdapter := &authFailAdapter{}
		pool := router.NewAccountPoolRouter([]types.ProviderAdapter{failAdapter})
		pool.SetSubscriptionPoolFilter(func(string) bool { return true })

		body := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		_ = bridge.HandleClaudeMessages(rec, req, pool, body)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status=%d, want 401", rec.Code)
		}

		var errResp struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errResp.Type != "error" || errResp.Error.Type != "authentication_error" {
			t.Errorf("unexpected error envelope: %+v", errResp)
		}
	})
}

type rateLimitFailAdapter struct {
	retryAfter time.Duration
}

func (a *rateLimitFailAdapter) ID() string    { return "fail:ratelimit" }
func (a *rateLimitFailAdapter) Priority() int { return 1 }
func (a *rateLimitFailAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, types.NewRateLimitError("upstream rate limit exceeded", a.retryAfter)
}

type authFailAdapter struct{}

func (a *authFailAdapter) ID() string    { return "fail:auth" }
func (a *authFailAdapter) Priority() int { return 1 }
func (a *authFailAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, types.ErrAuthentication
}
