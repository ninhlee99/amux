package guard

import (
	"testing"

	"amux-accounts/pkg/types"
)

func TestDetectAndPruneLoop(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "initial prompt"},
			// Repetition 1
			{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Name: "view_file", Arguments: `{"path":"main.go"}`}}},
			{Role: "tool", ToolCallID: "c1", Content: "package main"},
			// Repetition 2
			{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c2", Name: "view_file", Arguments: `{"path":"main.go"}`}}},
			{Role: "tool", ToolCallID: "c2", Content: "package main"},
			// Repetition 3
			{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c3", Name: "view_file", Arguments: `{"path":"main.go"}`}}},
			{Role: "tool", ToolCallID: "c3", Content: "package main"},
		},
	}

	detected, desc := DetectAndPruneLoop(req)
	if !detected {
		t.Fatalf("expected loop to be detected, got false")
	}
	if desc == "" {
		t.Errorf("expected non-empty description")
	}
	// Initial message (1) + 1st repetition (2) + warning note (1) + last repetition (2) = 6 messages (down from 7)
	if len(req.Messages) >= 7 {
		t.Errorf("expected pruned messages < 7, got %d", len(req.Messages))
	}
}
