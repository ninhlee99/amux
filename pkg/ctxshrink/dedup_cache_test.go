package ctxshrink

import (
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestGlobalToolDeduplicator_IdenticalOutputs(t *testing.T) {
	dedup := NewGlobalToolDeduplicator(100, 1*time.Hour)

	largeFileContent := strings.Repeat("func ProcessData() error { return nil }\n", 30) // ~1200 runes

	msgs := []types.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "read file main.go"},
		{
			Role:       "tool",
			ToolCallID: "call_read_1",
			Content:    largeFileContent,
		},
		{Role: "assistant", Content: "I read main.go"},
		{Role: "user", Content: "read file main.go again to double check"},
		{
			Role:       "tool",
			ToolCallID: "call_read_2",
			Content:    largeFileContent,
		},
		{Role: "assistant", Content: "Confirmed."},
		{Role: "user", Content: "Final question"},
		{
			Role:       "tool",
			ToolCallID: "call_read_3",
			Content:    largeFileContent, // This is the latest tool result (protected)
		},
	}

	// keepRecentToolResults = 1 -> only the last tool result (call_read_3) is protected.
	// call_read_1 is first seen.
	// call_read_2 is duplicate of call_read_1 -> should be deduplicated!
	deduped := dedup.DeduplicateMessages(msgs, 1)

	if len(deduped) != len(msgs) {
		t.Fatalf("expected same message count, got %d vs %d", len(deduped), len(msgs))
	}

	// call_read_1 is first occurrence -> kept full
	if deduped[2].Content != largeFileContent {
		t.Errorf("expected first occurrence to remain full, got: %s", deduped[2].Content)
	}

	// call_read_2 is second occurrence -> deduplicated with pointer
	if !strings.Contains(deduped[5].Content, "[amux dedup-cache:") || !strings.Contains(deduped[5].Content, "identical content omitted") {
		t.Errorf("expected second occurrence to be deduplicated, got: %s", deduped[5].Content)
	}

	// call_read_3 is recent (protected) -> kept full
	if deduped[8].Content != largeFileContent {
		t.Errorf("expected latest tool result to remain full, got: %s", deduped[8].Content)
	}

	// Verify token savings
	origTokens := EstimateMessagesTokens(msgs)
	dedupTokens := EstimateMessagesTokens(deduped)
	if dedupTokens >= origTokens {
		t.Errorf("expected dedup tokens (%d) < orig tokens (%d)", dedupTokens, origTokens)
	}
}

func TestGlobalToolDeduplicator_ShortOutputsNotTouched(t *testing.T) {
	dedup := NewGlobalToolDeduplicator(300, 1*time.Hour)

	shortContent := "error: file not found"
	msgs := []types.ChatMessage{
		{Role: "tool", ToolCallID: "c1", Content: shortContent},
		{Role: "tool", ToolCallID: "c2", Content: shortContent},
		{Role: "tool", ToolCallID: "c3", Content: shortContent},
	}

	deduped := dedup.DeduplicateMessages(msgs, 1)
	for i, m := range deduped {
		if m.Content != shortContent {
			t.Errorf("message %d was modified unexpectedly: %s", i, m.Content)
		}
	}
}
