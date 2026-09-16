package tools

import (
	"encoding/json"
	"testing"

	"amux-accounts/pkg/types"
)

func TestClaudePromptCaching_SystemAndToolsAndTail(t *testing.T) {
	req := &types.ChatRequest{
		Model:              "claude-3-7-sonnet-20250219",
		SystemCacheControl: true,
		Tools: []types.ToolDef{
			{Name: "bash", Description: "Run command", InputSchema: json.RawMessage(`{}`)},
			{Name: "read_file", Description: "Read file", InputSchema: json.RawMessage(`{}`)},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "System prompt"},
			{Role: "user", Content: "Turn 1"},
			{Role: "assistant", Content: "Turn 1 reply"},
			{Role: "user", Content: "Turn 2"},
		},
	}

	b, err := MarshalClaudeMessagesRequest(req, "")
	if err != nil {
		t.Fatalf("MarshalClaudeMessagesRequest failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	// 1. System must be formatted as blocks with cache_control: {"type": "ephemeral"}
	sysBlocks, ok := payload["system"].([]any)
	if !ok || len(sysBlocks) != 1 {
		t.Fatalf("expected system blocks, got: %v", payload["system"])
	}
	s0 := sysBlocks[0].(map[string]any)
	if s0["type"] != "text" || s0["text"] != "System prompt" {
		t.Errorf("expected text 'System prompt', got: %v", s0["text"])
	}
	cc, ok := s0["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Errorf("expected cache_control ephemeral on system block, got: %v", s0["cache_control"])
	}

	// 2. The last tool must have cache_control: {"type": "ephemeral"}
	toolsList, ok := payload["tools"].([]any)
	if !ok || len(toolsList) != 2 {
		t.Fatalf("expected 2 tools, got: %v", payload["tools"])
	}
	lastTool := toolsList[1].(map[string]any)
	tcc, ok := lastTool["cache_control"].(map[string]any)
	if !ok || tcc["type"] != "ephemeral" {
		t.Errorf("expected cache_control on last tool, got: %v", lastTool["cache_control"])
	}

	// 3. The second-to-last user turn must have cache_control: {"type": "ephemeral"}
	msgsList, ok := payload["messages"].([]any)
	if !ok || len(msgsList) != 3 {
		t.Fatalf("expected 3 coalesced messages, got %d", len(msgsList))
	}
	// Index 1 is assistant, Index 0 is user Turn 1, Index 2 is user Turn 2
	// Target turn before the latest turn (Turn 1 reply assistant at index 1)
	m1 := msgsList[1].(map[string]any)
	content1 := m1["content"].([]any)
	lastBlk := content1[len(content1)-1].(map[string]any)
	mcc, ok := lastBlk["cache_control"].(map[string]any)
	if !ok || mcc["type"] != "ephemeral" {
		t.Errorf("expected cache_control on message turn before latest, got: %v", lastBlk["cache_control"])
	}
}

func TestThoughtSignature_CacheAndLookup(t *testing.T) {
	callID := "call_test_123"
	sig := "opaque_crypto_signature_abc"

	RecordThoughtSignature(callID, sig)

	got := LookupThoughtSignature(callID)
	if got != sig {
		t.Fatalf("expected %q, got %q", sig, got)
	}

	if LookupThoughtSignature("nonexistent") != "" {
		t.Fatalf("expected empty for nonexistent ID")
	}
}

func TestOpenAIToolCalls_AttachThoughtSignature(t *testing.T) {
	callID := "call_gemini_xyz"
	sig := "sig_gemini_456"
	RecordThoughtSignature(callID, sig)

	calls := []types.ToolCall{
		{
			ID:        callID,
			Name:      "Bash",
			Arguments: `{"command":"pwd"}`,
		},
	}

	oc := ToOpenAIToolCalls(calls)
	if len(oc) != 1 {
		t.Fatalf("expected 1 call, got %d", len(oc))
	}
	if oc[0].ExtraContent == nil || oc[0].ExtraContent.Google.ThoughtSignature != sig {
		t.Fatalf("expected extra_content with Google thought_signature, got: %+v", oc[0].ExtraContent)
	}
}
