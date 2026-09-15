package ctxshrink

import (
	"fmt"
	"strings"

	"amux-accounts/pkg/types"
)

const (
	// toolResultKeepFull = last N tool results stay full-size.
	toolResultKeepFull = 2
	// toolResultMaxRunes for older tool outputs (head+tail).
	toolResultMaxRunes = 800
	toolResultHead     = 500
	toolResultTail     = 150
	// compactKeepTailTurns when TaskCompact / long hist.
	compactKeepTailTurns = 6
	compactMaxMessages   = 24

	// DefaultWebMaxTokens is the safe context ceiling for ChatGPT web,
	// Claude web, and Gemini web to prevent 413 / payload-too-large errors.
	// 20,000 tokens leaves a comfortable 5,000 token margin for generation.
	DefaultWebMaxTokens = 20000

	// AbsoluteMaxWebRunes ensures request body string never exceeds ~85k runes
	// under any circumstances (preventing web proxy body buffer overflow).
	AbsoluteMaxWebRunes = 85000
)

// EstimateTokens provides a realistic BPE-aligned token approximation for strings.
func EstimateTokens(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	// Heuristic: ~3.8 runes per token for mixed code, tool syntax and natural language.
	tok := (n * 10) / 38
	if tok < 1 {
		tok = 1
	}
	return tok
}

// EstimateMessagesTokens calculates the aggregate tokens for a message slice
// including role framing and tool call arguments overhead.
func EstimateMessagesTokens(msgs []types.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content) + 4 // role framing
		for _, tc := range m.ToolCalls {
			total += EstimateTokens(tc.Name) + EstimateTokens(tc.Arguments) + 6
		}
	}
	return total
}

// CompactMessages truncates older tool results to cut web/API token burn.
// Last toolResultKeepFull tool messages stay intact.
func CompactMessages(msgs []types.ChatMessage) []types.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}
	toolIdx := make([]int, 0, 8)
	for i, m := range msgs {
		if strings.EqualFold(m.Role, "tool") {
			toolIdx = append(toolIdx, i)
		}
	}
	keepFrom := 0
	if len(toolIdx) > toolResultKeepFull {
		keepFrom = len(toolIdx) - toolResultKeepFull
	}
	fullSet := map[int]bool{}
	for _, i := range toolIdx[keepFrom:] {
		fullSet[i] = true
	}

	out := make([]types.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if !strings.EqualFold(out[i].Role, "tool") || fullSet[i] {
			continue
		}
		out[i].Content = truncateRunes(out[i].Content, toolResultMaxRunes, toolResultHead, toolResultTail)
	}
	return out
}

// CompactTranscript aggressively shrinks long histories for TaskCompact:
// keep system + first user + tail turns; summarize dropped middle as one note.
func CompactTranscript(msgs []types.ChatMessage) []types.ChatMessage {
	return CompactTranscriptWithTail(msgs, compactKeepTailTurns)
}

// CompactTranscriptWithTail keeps system messages, the first user prompt (initial task goal),
// and the last tailTurns messages. Middle turns are summarized into a concise handoff note.
func CompactTranscriptWithTail(msgs []types.ChatMessage, tailTurns int) []types.ChatMessage {
	if len(msgs) <= tailTurns+2 {
		return CompactMessages(msgs)
	}
	var sys []types.ChatMessage
	var rest []types.ChatMessage
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "system") {
			sys = append(sys, m)
			continue
		}
		rest = append(rest, m)
	}
	if len(rest) <= tailTurns+1 {
		return CompactMessages(msgs)
	}
	first := rest[0]
	tail := rest[len(rest)-tailTurns:]
	dropped := len(rest) - 1 - tailTurns
	note := types.ChatMessage{
		Role:    "user",
		Content: fmt.Sprintf("[compact] %d earlier turns omitted to maintain token budget. Continue from latest context.", dropped),
	}
	out := make([]types.ChatMessage, 0, len(sys)+3+len(tail))
	out = append(out, sys...)
	out = append(out, first, note)
	out = append(out, tail...)
	return CompactMessages(out)
}

// FitMessagesToTokenBudget progressively compacts a message slice until its
// estimated tokens fit strictly inside maxTokens.
//
// Progressive levels:
//  1. Level 1: Standard tool compaction (keep last 2 full, older to 800 runes).
//  2. Level 2: Deep tool compaction (older tools to 300 runes, older assistant prose trimmed).
//  3. Level 3: Sliding window (System + First user goal + 6 tail turns).
//  4. Level 4: Tighter sliding window (Tail down to 4 turns, then 2 turns).
//  5. Level 5: Emergency capping on any single oversized message.
func FitMessagesToTokenBudget(msgs []types.ChatMessage, maxTokens int) []types.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}
	if maxTokens <= 0 {
		maxTokens = DefaultWebMaxTokens
	}

	// Level 1: Standard tool compaction
	res := CompactMessages(msgs)
	if EstimateMessagesTokens(res) <= maxTokens {
		return res
	}

	// Level 2: Deep tool compaction
	res = compactDeep(res)
	if EstimateMessagesTokens(res) <= maxTokens {
		return res
	}

	// Level 3 & 4: Progressive sliding window
	for tail := 6; tail >= 2; tail -= 2 {
		res = CompactTranscriptWithTail(res, tail)
		if EstimateMessagesTokens(res) <= maxTokens {
			return res
		}
	}

	// Level 5: Emergency cap on single large messages
	return emergencyCapMessages(res, maxTokens)
}

func compactDeep(msgs []types.ChatMessage) []types.ChatMessage {
	out := make([]types.ChatMessage, len(msgs))
	copy(out, msgs)

	toolIdx := make([]int, 0, 8)
	for i, m := range out {
		if strings.EqualFold(m.Role, "tool") {
			toolIdx = append(toolIdx, i)
		}
	}
	keepFrom := 0
	if len(toolIdx) > 1 {
		keepFrom = len(toolIdx) - 1 // keep only the very last tool result full
	}
	fullSet := map[int]bool{}
	for _, i := range toolIdx[keepFrom:] {
		fullSet[i] = true
	}

	for i := range out {
		if strings.EqualFold(out[i].Role, "tool") && !fullSet[i] {
			out[i].Content = truncateRunes(out[i].Content, 300, 200, 80)
		} else if strings.EqualFold(out[i].Role, "assistant") && len(out[i].ToolCalls) == 0 && i < len(out)-2 {
			// Older intermediate assistant thoughts can be trimmed
			out[i].Content = truncateRunes(out[i].Content, 1000, 600, 300)
		}
	}
	return out
}

func emergencyCapMessages(msgs []types.ChatMessage, maxTokens int) []types.ChatMessage {
	perMsgCapRunes := (maxTokens * 35) / (10 * (len(msgs) + 1))
	if perMsgCapRunes < 500 {
		perMsgCapRunes = 500
	}
	out := make([]types.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		// Never cap system prompt or last message
		if strings.EqualFold(out[i].Role, "system") || i == len(out)-1 {
			continue
		}
		if len([]rune(out[i].Content)) > perMsgCapRunes {
			head := perMsgCapRunes * 6 / 10
			tail := perMsgCapRunes * 3 / 10
			out[i].Content = truncateRunes(out[i].Content, perMsgCapRunes, head, tail)
		}
	}
	return out
}

func truncateRunes(s string, max, head, tail int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if head+tail >= len(r) {
		return s
	}
	return string(r[:head]) + "\n... [truncated] ...\n" + string(r[len(r)-tail:])
}
