package ctxshrink

import (
	"encoding/json"
	"testing"
)

func TestOptimizeAnthropicPromptCaching(t *testing.T) {
	rawInput := `{
		"model": "claude-3-7-sonnet-20250219",
		"system": "You are a very helpful programming assistant with extensive context and instructions that exceed one hundred characters in total length for this test.",
		"messages": [
			{"role": "user", "content": "Hello, can you help me write a function?"},
			{"role": "assistant", "content": "Sure, I can help you with that. What function do you need?"},
			{"role": "user", "content": "A function that parses JSON in Go."}
		],
		"tools": [
			{
				"name": "bash",
				"description": "Execute a shell command",
				"input_schema": {"type": "object"}
			},
			{
				"name": "read_file",
				"description": "Read file contents",
				"input_schema": {"type": "object"}
			}
		]
	}`

	optimized, modified := OptimizeAnthropicPromptCaching([]byte(rawInput))
	if !modified {
		t.Fatalf("expected prompt caching to be applied, got modified = false")
	}

	var parsed map[string]any
	if err := json.Unmarshal(optimized, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// 1. Check system is converted to array with cache_control
	sysList, ok := parsed["system"].([]any)
	if !ok || len(sysList) == 0 {
		t.Fatalf("expected system to be converted to list of blocks, got %T", parsed["system"])
	}
	sysBlock := sysList[0].(map[string]any)
	if sysBlock["cache_control"] == nil {
		t.Errorf("expected cache_control on system prompt block")
	}

	// 2. Check tools: second tool (last tool) has cache_control
	toolsList, ok := parsed["tools"].([]any)
	if !ok || len(toolsList) != 2 {
		t.Fatalf("expected 2 tools, got %v", toolsList)
	}
	lastTool := toolsList[1].(map[string]any)
	if lastTool["cache_control"] == nil {
		t.Errorf("expected cache_control on last tool")
	}
	firstTool := toolsList[0].(map[string]any)
	if firstTool["cache_control"] != nil {
		t.Errorf("expected first tool to not have cache_control")
	}

	// 3. Check messages: penultimate message (index 1: assistant) has cache_control
	msgsList, ok := parsed["messages"].([]any)
	if !ok || len(msgsList) != 3 {
		t.Fatalf("expected 3 messages, got %v", msgsList)
	}
	penultTurn := msgsList[1].(map[string]any)
	contentList, ok := penultTurn["content"].([]any)
	if !ok || len(contentList) == 0 {
		t.Fatalf("expected penultimate turn content to be converted to list, got %T", penultTurn["content"])
	}
	contentBlock := contentList[0].(map[string]any)
	if contentBlock["cache_control"] == nil {
		t.Errorf("expected cache_control on penultimate message content block")
	}

	// 4. Verify total breakpoint count is <= 4
	totalBreakpoints := countBreakpoints(parsed)
	if totalBreakpoints > MaxAnthropicCacheBreakpoints {
		t.Errorf("expected at most %d breakpoints, got %d", MaxAnthropicCacheBreakpoints, totalBreakpoints)
	}
	if totalBreakpoints != 3 {
		t.Errorf("expected exactly 3 breakpoints, got %d", totalBreakpoints)
	}

	// Test idempotency and boundary limit (if already 4 breakpoints, do nothing)
	alreadyFour := `{
		"system": [{"type": "text", "text": "sys", "cache_control": {"type": "ephemeral"}}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "1", "cache_control": {"type": "ephemeral"}}]},
			{"role": "assistant", "content": [{"type": "text", "text": "2", "cache_control": {"type": "ephemeral"}}]},
			{"role": "user", "content": [{"type": "text", "text": "3", "cache_control": {"type": "ephemeral"}}]}
		]
	}`
	_, mod4 := OptimizeAnthropicPromptCaching([]byte(alreadyFour))
	if mod4 {
		t.Errorf("expected no modification when already at 4 breakpoints")
	}
}
