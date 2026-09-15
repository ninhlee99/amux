package ctxshrink

import (
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestCompactMessages_TruncatesOldTools(t *testing.T) {
	big := strings.Repeat("x", 2000)
	msgs := []types.ChatMessage{
		{Role: "tool", Content: big},
		{Role: "tool", Content: big},
		{Role: "tool", Content: "recent-a"},
		{Role: "tool", Content: "recent-b"},
	}
	got := CompactMessages(msgs)
	if !strings.Contains(got[0].Content, "[truncated]") {
		t.Fatal("old tool must truncate")
	}
	if got[2].Content != "recent-a" || got[3].Content != "recent-b" {
		t.Fatal("last 2 tools must stay full")
	}
}

func TestCompactTranscript_DropsMiddle(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "system", Content: "sys"}}
	for i := 0; i < 30; i++ {
		msgs = append(msgs, types.ChatMessage{Role: "user", Content: "u"})
		msgs = append(msgs, types.ChatMessage{Role: "assistant", Content: "a"})
	}
	got := CompactTranscript(msgs)
	if len(got) >= len(msgs) {
		t.Fatalf("expected shrink %d → %d", len(msgs), len(got))
	}
	joined := ""
	for _, m := range got {
		joined += m.Content
	}
	if !strings.Contains(joined, "[compact]") {
		t.Fatal("missing compact note")
	}
}
