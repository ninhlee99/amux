package ctxshrink

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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

// SemanticSummarizerFunc optionally summarizes dropped middle turns into a concise
// semantic overview. If registered, it is called during account switches; on any failure
// or timeout, amux falls back immediately to extractFastSemanticHandoff (<0.01ms rule-based).
type SemanticSummarizerFunc func(middle []types.ChatMessage) (string, error)

var (
	summarizerMu     sync.RWMutex
	globalSummarizer SemanticSummarizerFunc
)

// SetGlobalSemanticSummarizer sets the pluggable summarizer (e.g. backed by a cheap/flash provider).
func SetGlobalSemanticSummarizer(fn SemanticSummarizerFunc) {
	summarizerMu.Lock()
	defer summarizerMu.Unlock()
	globalSummarizer = fn
}

func getGlobalSemanticSummarizer() SemanticSummarizerFunc {
	summarizerMu.RLock()
	defer summarizerMu.RUnlock()
	return globalSummarizer
}

// CompactForAccountSwitch compacts a conversation history when switching to a new account,
// dramatically reducing cold-start tokens (and avoiding paying cache creation on old logs).
// It preserves:
// 1. System instructions (so model personas/rules remain 100% intact).
// 2. The initial user goal/prompt (the root task).
// 3. Compacts older middle tool results & turns into an explicit handoff note.
// 4. Preserves the recent tail turns intact (with full context/tool results)
//    so the model can immediately continue without losing recent state.
// 5. Safely converts any orphaned tool_results whose tool_use was dropped into
// CompactForAccountSwitch compacts a conversation history when switching to a new account.
func CompactForAccountSwitch(msgs []types.ChatMessage, tailTurns int) []types.ChatMessage {
	return CompactForAccountSwitchProject("", msgs, tailTurns)
}

// CompactForAccountSwitchProject compacts a conversation history when switching to a new account,
// strictly isolated to the specified project.
func CompactForAccountSwitchProject(project string, msgs []types.ChatMessage, tailTurns int) []types.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}
	if tailTurns <= 0 {
		tailTurns = compactKeepTailTurns
	}
	// First run project-scoped tool deduplication to eliminate repeated identical tool outputs
	msgs = GlobalDeduplicator().DeduplicateMessages(project, msgs, 2)

	// If message count is already small and estimated tokens are under 8000,
	// just run standard tool compaction without dropping any turns.
	if len(msgs) <= tailTurns+2 && EstimateMessagesTokens(msgs) < 8000 {
		return CompactMessages(msgs)
	}

	var sys []types.ChatMessage
	var rest []types.ChatMessage
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "system") {
			sys = append(sys, m)
		} else {
			rest = append(rest, m)
		}
	}

	if len(rest) <= tailTurns+1 {
		return CompactMessages(msgs)
	}

	firstUser := rest[0]
	tail := rest[len(rest)-tailTurns:]
	droppedCount := len(rest) - 1 - tailTurns

	// Map all tool call IDs defined inside the tail turns
	tailToolIDs := make(map[string]bool)
	for _, m := range tail {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				tailToolIDs[tc.ID] = true
			}
		}
	}

	// Sanitize tail: Any tool_result whose tool_use was dropped in the middle
	// must be converted into a safe user message containing the tool result text,
	// so Anthropic/OpenAI APIs will never reject with "orphaned tool_result".
	sanitizedTail := make([]types.ChatMessage, 0, len(tail))
	for _, m := range tail {
		if strings.EqualFold(m.Role, "tool") && !tailToolIDs[m.ToolCallID] {
			sanitizedTail = append(sanitizedTail, types.ChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("[Prior Tool Output - %s]:\n%s", m.ToolCallID, m.Content),
			})
		} else {
			sanitizedTail = append(sanitizedTail, m)
		}
	}

	middleTurns := rest[1 : len(rest)-tailTurns]
	
	// Try semantic summarizer if available; fallback instantly to rule-based
	var summary string
	if summarizer := getGlobalSemanticSummarizer(); summarizer != nil {
		if s, err := summarizer(middleTurns); err == nil && strings.TrimSpace(s) != "" {
			summary = fmt.Sprintf("[amux switch handoff] Session rotated account. %d intermediate turns summarized:\n%s\nPlease continue seamlessly from the latest context below.", droppedCount, strings.TrimSpace(s))
		}
	}
	if summary == "" {
		summary = extractFastSemanticHandoff(middleTurns, droppedCount)
	}

	handoffNote := types.ChatMessage{
		Role:    "user",
		Content: summary,
	}

	out := make([]types.ChatMessage, 0, len(sys)+3+len(sanitizedTail))
	out = append(out, sys...)
	out = append(out, firstUser)
	out = append(out, handoffNote)
	out = append(out, sanitizedTail...)

	return CompactMessages(out)
}

// extractFastSemanticHandoff builds a fast, lightweight summary (<250 tokens) of middle turns
// without external LLM latency or dependencies. It extracts key files touched, tool actions executed,
// and user intents so the destination account understands prior progress instantly.
func extractFastSemanticHandoff(middle []types.ChatMessage, droppedCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[amux switch handoff] Session rotated account. %d intermediate turns compacted to minimize token burn.\n", droppedCount))

	// Track unique tools, files touched, commands executed, and user intents
	toolActions := make(map[string]int)
	filesTouched := make(map[string]bool)
	var commandsRun []string
	var userDirectives []string

	for _, m := range middle {
		if strings.EqualFold(m.Role, "user") {
			trimmed := strings.TrimSpace(m.Content)
			if trimmed != "" && len(userDirectives) < 3 {
				// Keep first line or up to 100 runes
				lines := strings.Split(trimmed, "\n")
				firstLine := strings.TrimSpace(lines[0])
				if len([]rune(firstLine)) > 80 {
					firstLine = string([]rune(firstLine)[:80]) + "..."
				}
				userDirectives = append(userDirectives, firstLine)
			}
		}
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				toolActions[tc.Name]++
			}
			if len(tc.Arguments) > 0 && json.Valid([]byte(tc.Arguments)) {
				var argsMap map[string]any
				if err := json.Unmarshal([]byte(tc.Arguments), &argsMap); err == nil {
					for _, k := range []string{"path", "file", "TargetFile", "AbsolutePath", "filepath", "target_file", "FilePath"} {
						if v, ok := argsMap[k].(string); ok && strings.TrimSpace(v) != "" {
							filesTouched[strings.TrimSpace(v)] = true
						}
					}
					for _, k := range []string{"command", "CommandLine", "cmd"} {
						if v, ok := argsMap[k].(string); ok && strings.TrimSpace(v) != "" && len(commandsRun) < 4 {
							cmdTrim := strings.TrimSpace(v)
							if len([]rune(cmdTrim)) > 60 {
								cmdTrim = string([]rune(cmdTrim)[:60]) + "..."
							}
							commandsRun = append(commandsRun, cmdTrim)
						}
					}
				}
			}
		}
	}

	if len(userDirectives) > 0 {
		sb.WriteString("Recent Directives:\n")
		for _, d := range userDirectives {
			sb.WriteString("- ")
			sb.WriteString(d)
			sb.WriteString("\n")
		}
	}

	if len(filesTouched) > 0 {
		sb.WriteString("Files Referenced in Prior Turns: ")
		var fileList []string
		for f := range filesTouched {
			if len(fileList) >= 6 {
				fileList = append(fileList, fmt.Sprintf("and %d more", len(filesTouched)-6))
				break
			}
			fileList = append(fileList, f)
		}
		sb.WriteString(strings.Join(fileList, ", "))
		sb.WriteString("\n")
	}

	if len(commandsRun) > 0 {
		sb.WriteString("Commands Executed: ")
		sb.WriteString(strings.Join(commandsRun, " | "))
		sb.WriteString("\n")
	}

	if len(toolActions) > 0 {
		sb.WriteString("Prior Actions: ")
		var acts []string
		for name, count := range toolActions {
			acts = append(acts, fmt.Sprintf("%s (%d)", name, count))
		}
		sb.WriteString(strings.Join(acts, ", "))
		sb.WriteString("\n")
	}

	sb.WriteString("Please continue seamlessly from the latest context below.")
	return sb.String()
}
