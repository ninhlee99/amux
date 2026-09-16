package ctxshrink

import (
	"fmt"
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

func TestFitMessagesToTokenBudget_LargeTranscriptShrunkUnder20k(t *testing.T) {
	// Simulate a massive 50-turn agent loop with large tool results (~60,000 tokens)
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are an expert agent."},
		{Role: "user", Content: "Initial Goal: Refactor the entire payments module."},
	}

	for i := 1; i <= 25; i++ {
		largeOutput := fmt.Sprintf("Turn %d: File content chunk:\n%s", i, strings.Repeat("line of code;\n", 500)) // ~7500 chars each
		msgs = append(msgs, types.ChatMessage{
			Role:      "assistant",
			Content:   fmt.Sprintf("Executing step %d", i),
			ToolCalls: []types.ToolCall{{ID: fmt.Sprintf("call_%d", i), Name: "Read", Arguments: `{"path":"pay.go"}`}},
		})
		msgs = append(msgs, types.ChatMessage{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call_%d", i),
			Content:    largeOutput,
		})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: "Continue to step 26"})

	initialTokens := EstimateMessagesTokens(msgs)
	if initialTokens < 30000 {
		t.Fatalf("expected massive transcript >30k tokens, got %d", initialTokens)
	}

	// Shrink with budget 20,000
	budget := 20000
	shrunk := FitMessagesToTokenBudget(msgs, budget)

	finalTokens := EstimateMessagesTokens(shrunk)
	if finalTokens > budget {
		t.Fatalf("final tokens %d exceeded budget %d", finalTokens, budget)
	}

	// Shrink with strict budget 3,000 (forces Level 3 sliding window)
	strictBudget := 3000
	strictShrunk := FitMessagesToTokenBudget(msgs, strictBudget)
	strictTokens := EstimateMessagesTokens(strictShrunk)
	if strictTokens > strictBudget {
		t.Fatalf("strict tokens %d exceeded budget %d", strictTokens, strictBudget)
	}

	// Verify System prompt and Initial User Goal are preserved
	if strictShrunk[0].Role != "system" || strictShrunk[0].Content != "You are an expert agent." {
		t.Errorf("system prompt was lost: %+v", strictShrunk[0])
	}
	if !strings.Contains(strictShrunk[1].Content, "Initial Goal: Refactor") {
		t.Errorf("initial user goal was lost: %+v", strictShrunk[1])
	}

	// Verify last user message is preserved
	lastMsg := strictShrunk[len(strictShrunk)-1]
	if lastMsg.Role != "user" || lastMsg.Content != "Continue to step 26" {
		t.Errorf("latest context user turn was lost: %+v", lastMsg)
	}

	// Verify compact notice exists in strict shrunk
	joined := ""
	for _, m := range strictShrunk {
		joined += m.Content + " "
	}
	if !strings.Contains(joined, "[compact]") {
		t.Errorf("expected [compact] handoff note in middle for strict budget")
	}
}

func TestFitMessagesToTokenBudget_EmergencyCapSingleGiantMessage(t *testing.T) {
	// A user pastes a single 150,000-character crash log or file
	giant := strings.Repeat("error stack trace line 12345;\n", 5000) // ~150k runes
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are a debugger."},
		{Role: "user", Content: giant},
		{Role: "user", Content: "Please explain the error."},
	}

	budget := 20000
	shrunk := FitMessagesToTokenBudget(msgs, budget)

	finalTokens := EstimateMessagesTokens(shrunk)
	if finalTokens > budget {
		t.Fatalf("emergency cap failed: tokens %d > budget %d", finalTokens, budget)
	}
}

func TestCompactForAccountSwitch(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are Claude Code assistant."},
		{Role: "user", Content: "Build the whole authentication system."},
	}

	// Add 20 turns of agent history with tools
	for i := 1; i <= 20; i++ {
		msgs = append(msgs, types.ChatMessage{
			Role:      "assistant",
			Content:   fmt.Sprintf("Running inspection step %d", i),
			ToolCalls: []types.ToolCall{{ID: fmt.Sprintf("call_%d", i), Name: "Bash", Arguments: `{"cmd":"ls"}`}},
		})
		msgs = append(msgs, types.ChatMessage{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call_%d", i),
			Content:    fmt.Sprintf("output of step %d: %s", i, strings.Repeat("long log output line;\n", 100)),
		})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: "Now implement the JWT refresh handler."})

	origTokens := EstimateMessagesTokens(msgs)
	if origTokens < 10000 {
		t.Fatalf("expected initial tokens > 10000, got %d", origTokens)
	}

	// Compact for account switch with 4 tail turns
	compacted := CompactForAccountSwitch(msgs, 4)

	newTokens := EstimateMessagesTokens(compacted)
	if newTokens > origTokens/3 {
		t.Fatalf("expected compacted tokens to be < 1/3 of original, got orig=%d new=%d", origTokens, newTokens)
	}

	// System prompt must be preserved at index 0
	if compacted[0].Role != "system" || compacted[0].Content != "You are Claude Code assistant." {
		t.Fatalf("system prompt corrupted: %+v", compacted[0])
	}

	// First user task goal must be preserved at index 1
	if compacted[1].Role != "user" || compacted[1].Content != "Build the whole authentication system." {
		t.Fatalf("initial user goal lost: %+v", compacted[1])
	}

	// Handoff note must be present
	joined := ""
	for _, m := range compacted {
		joined += m.Content + " "
	}
	if !strings.Contains(joined, "[amux switch handoff]") {
		t.Fatalf("missing [amux switch handoff] note")
	}

	// Last user message must be preserved
	last := compacted[len(compacted)-1]
	if last.Role != "user" || last.Content != "Now implement the JWT refresh handler." {
		t.Fatalf("latest user context lost: %+v", last)
	}

	// Check that no tool message in compacted has an orphan tool_call_id
	tailToolIDs := make(map[string]bool)
	for _, m := range compacted {
		for _, tc := range m.ToolCalls {
			tailToolIDs[tc.ID] = true
		}
	}
	for _, m := range compacted {
		if strings.EqualFold(m.Role, "tool") && !tailToolIDs[m.ToolCallID] {
			t.Fatalf("found orphaned tool message: %+v", m)
		}
	}
}

func TestSemanticSummarizerFallback(t *testing.T) {
	var msgs []types.ChatMessage
	msgs = append(msgs, types.ChatMessage{Role: "system", Content: "System prompt"})
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: "Initial goal"})
	for i := 0; i < 15; i++ {
		msgs = append(msgs, types.ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("Intermediate reasoning step %d with detailed explanation", i),
		})
		msgs = append(msgs, types.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("User feedback on step %d", i),
		})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: "Final prompt"})

	// 1. Test with custom summarizer
	SetGlobalSemanticSummarizer(func(middle []types.ChatMessage) (string, error) {
		return "Consensus: JWT and Scrypt encryption fully finalized.", nil
	})
	defer SetGlobalSemanticSummarizer(nil)

	res := CompactForAccountSwitch(msgs, 4)
	joined := ""
	for _, m := range res {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "JWT and Scrypt encryption fully finalized") {
		t.Fatalf("expected semantic summary in compacted transcript, got:\n%s", joined)
	}

	// 2. Test fallback when summarizer errors
	SetGlobalSemanticSummarizer(func(middle []types.ChatMessage) (string, error) {
		return "", fmt.Errorf("timeout or network error")
	})

	resFallback := CompactForAccountSwitch(msgs, 4)
	joinedFallback := ""
	for _, m := range resFallback {
		joinedFallback += m.Content + "\n"
	}
	if !strings.Contains(joinedFallback, "[amux switch handoff]") {
		t.Fatalf("expected rule-based fallback handoff, got:\n%s", joinedFallback)
	}
}

