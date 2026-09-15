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
)

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
	if len(msgs) <= compactMaxMessages {
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
	if len(rest) <= compactKeepTailTurns+1 {
		return CompactMessages(msgs)
	}
	first := rest[0]
	tail := rest[len(rest)-compactKeepTailTurns:]
	dropped := len(rest) - 1 - compactKeepTailTurns
	note := types.ChatMessage{
		Role:    "user",
		Content: fmt.Sprintf("[compact] %d earlier turns omitted to save tokens. Continue from latest context.", dropped),
	}
	out := make([]types.ChatMessage, 0, len(sys)+3+len(tail))
	out = append(out, sys...)
	out = append(out, first, note)
	out = append(out, tail...)
	return CompactMessages(out)
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
