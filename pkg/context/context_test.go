package context_test

import (
	"testing"

	"amux-accounts/pkg/context"
	"amux-accounts/pkg/types"
)

func TestContext_SafeCompaction(t *testing.T) {
	req := &types.ChatRequest{
		Tools: []types.ToolDef{
			{Name: "Bash", Description: "Execute shell"},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are assistant."},
			{Role: "user", Content: "Initial prompt."},
			{Role: "tool", Content: "Very long old tool output " + string(make([]byte, 2000))},
			{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{ID: "call_latest", Name: "Bash", Arguments: `{"command":"pwd"}`},
				},
			},
		},
	}

	// 1. Normal IDE operation -> Disabled
	compacted := context.CompactConversation(req, context.SafeCompactionOption{MidSessionRotation: false})
	if compacted {
		t.Errorf("compaction should be disabled during normal IDE operation")
	}

	// 2. Mid-session rotation -> Allowed
	compacted = context.CompactConversation(req, context.SafeCompactionOption{MidSessionRotation: true})
	if !compacted {
		t.Fatalf("expected compaction to run on mid-session rotation")
	}

	// Verify Tools schema is 100% untouched
	if len(req.Tools) != 1 || req.Tools[0].Name != "Bash" {
		t.Fatalf("tools schema was mutated or trimmed")
	}

	// Verify the latest tool call turn was preserved intact
	lastMsg := req.Messages[len(req.Messages)-1]
	if len(lastMsg.ToolCalls) == 0 || lastMsg.ToolCalls[0].ID != "call_latest" {
		t.Fatalf("latest tool turn was lost: %v", lastMsg)
	}
}
