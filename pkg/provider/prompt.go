package provider

import (
	"regexp"
	"strings"

	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// PromptWithSystem prepends all system turns to userPrompt. Web adapters that
// only POST a single completion string (Claude/ChatGPT/Gemini web) must use
// this so Claude Code / client system prompts are not dropped.
func PromptWithSystem(messages []types.ChatMessage, userPrompt string) string {
	var sys strings.Builder
	for _, m := range messages {
		if strings.EqualFold(m.Role, "system") && m.Content != "" {
			if sys.Len() > 0 {
				sys.WriteString("\n\n")
			}
			sys.WriteString(m.Content)
		}
	}
	if sys.Len() == 0 {
		return userPrompt
	}
	return sys.String() + "\n\n" + userPrompt
}

const contextHandoffPreamble = `[xfer] Continue. [Tool result] = real CLI output. Emit <tool_call> if you need files/commands; else answer.

`

// WebBackendPrompt builds the single string web UIs accept.
//
// Token policy:
//   - Cold start / new thread: flatten (compacted) history once.
//   - Continuing server thread + tools: delta only (recent tool results + last user)
//     so we do not re-pay full hist every turn.
//   - Interactive am chat (!FullContext): last user (+ system) when continuing.
func WebBackendPrompt(req *types.ChatRequest, continuingThread bool) string {
	if req == nil {
		return ""
	}
	msgs := req.Messages
	if len(req.Tools) > 0 {
		// Drop Claude/Cursor harness (huge + contradicts web tools).
		// Catalog comes from this request's tools[] — new MCP/plugin/Skill
		// show up automatically, no proxy code change.
		msgs = slimWebMessages(msgs)
	}
	msgs = ctxshrink.CompactMessages(msgs)

	var body string
	useDelta := req.FullContext && continuingThread && len(req.Tools) > 0 && historyHasToolTurns(msgs)
	switch {
	case useDelta:
		body = BuildDeltaWebPrompt(msgs)
		if body == "" {
			body = BuildConcatenatedPrompt(msgs)
		}
		body = contextHandoffPreamble + body
	case req.FullContext:
		body = BuildConcatenatedPrompt(msgs)
		if body == "" {
			return ""
		}
		body = contextHandoffPreamble + body
	case continuingThread:
		body = PromptWithSystem(msgs, lastUserPrompt(msgs))
	default:
		body = BuildConcatenatedPrompt(msgs)
	}
	if len(req.Tools) == 0 {
		return body
	}
	closer := tools.WebCloser()
	preamble := tools.WebPreamble(req.Tools)
	if continuingThread {
		// Thread already saw full protocol; catalog-only saves ~2k tokens/turn.
		preamble = tools.WebCatalogOnly(req.Tools)
	}
	trimmedBody := strings.TrimSpace(body)
	if strings.HasSuffix(trimmedBody, "Assistant:") {
		trimmedBody = strings.TrimSuffix(trimmedBody, "Assistant:")
		body = strings.TrimSpace(trimmedBody) + "\n\n" + strings.TrimSpace(closer) + "\n\nAssistant: "
		return preamble + body
	}
	return preamble + body + closer
}

// historyHasToolTurns is true when the client already ran tools this session.
func historyHasToolTurns(msgs []types.ChatMessage) bool {
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "tool") || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// BuildDeltaWebPrompt sends only the latest user turn plus recent tool
// results/assistant tool calls — for live web threads that already hold prior context.
func BuildDeltaWebPrompt(messages []types.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	// Keep from the last user message that is NOT only a tool-result wrapper,
	// including subsequent assistant tool_calls and tool results.
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			start = i
			// Include immediately preceding assistant tool_calls if any.
			if i > 0 && strings.EqualFold(messages[i-1].Role, "assistant") && len(messages[i-1].ToolCalls) > 0 {
				start = i - 1
			}
			break
		}
	}
	// Also pull the trailing tool-result chain before that user if the last
	// messages are tool results (Claude Code pattern: user → assistant tools → tools → user).
	slice := messages[start:]
	// Prepend up to 2 prior tool results if start skipped them.
	if start > 0 {
		extra := 0
		for i := start - 1; i >= 0 && extra < 2; i-- {
			if strings.EqualFold(messages[i].Role, "tool") {
				slice = append([]types.ChatMessage{messages[i]}, slice...)
				extra++
				continue
			}
			break
		}
	}
	return BuildConcatenatedPrompt(slice)
}

// slimWebMessages drops client harness system turns. User/tool/assistant stay.
func slimWebMessages(msgs []types.ChatMessage) []types.ChatMessage {
	out := make([]types.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "system") && isClientHarness(m.Content) {
			continue
		}
		if strings.EqualFold(m.Role, "user") {
			c := stripWebUserNoise(m.Content)
			if c == "" {
				continue
			}
			m.Content = c
		}
		out = append(out, m)
	}
	return out
}

var (
	reSysReminder    = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>\s*`)
	reTotalTokens    = regexp.MustCompile(`(?s)<total_tokens>.*?</total_tokens>\s*`)
	reScratchpadHint = regexp.MustCompile(`(?im)^First privately list what you need next;[^\n]*\n?`)
	reHookNotice     = regexp.MustCompile(`(?im)^(?:SessionStart|UserPromptSubmit)\b[^\n]*\n?`)
)

func stripWebUserNoise(s string) string {
	s = reSysReminder.ReplaceAllString(s, "")
	s = reTotalTokens.ReplaceAllString(s, "")
	s = reScratchpadHint.ReplaceAllString(s, "")
	s = reHookNotice.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func isClientHarness(s string) bool {
	if strings.Contains(s, "You are Claude Code") || strings.Contains(s, "x-anthropic-billing-header") {
		return true
	}
	if strings.Contains(s, "You are Antigravity") || (strings.Contains(s, "Antigravity") && strings.Contains(s, "agentic")) {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(s, "permission mode") && strings.Contains(low, "tool") {
		return true
	}
	if len([]rune(s)) > 2500 && (strings.Contains(low, "available tools") || strings.Contains(low, "input_schema")) {
		return true
	}
	return false
}
