package provider

import (
	"regexp"
	"strings"

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
// Prompt is kept intact without token-cutting or message-truncation layers,
// preserving full agent harness, skills, guidelines, and conversation history.
func WebBackendPrompt(req *types.ChatRequest, continuingThread bool) string {
	if req == nil {
		return ""
	}
	msgs := req.Messages

	var body string
	if req.FullContext {
		body = BuildConcatenatedPrompt(msgs)
		if body == "" {
			return ""
		}
		body = contextHandoffPreamble + body
	} else if continuingThread {
		body = PromptWithSystem(msgs, lastUserPrompt(msgs))
	} else {
		body = BuildConcatenatedPrompt(msgs)
	}

	if len(req.Tools) == 0 {
		return body
	}
	closer := tools.WebCloser()
	preamble := tools.WebPreambleForRequest(req)
	trimmedBody := strings.TrimSpace(body)
	var finalPrompt string
	if strings.HasSuffix(trimmedBody, "Assistant:") {
		trimmedBody = strings.TrimSuffix(trimmedBody, "Assistant:")
		body = strings.TrimSpace(trimmedBody) + "\n\n" + strings.TrimSpace(closer) + "\n\nAssistant: "
		finalPrompt = preamble + body
	} else {
		finalPrompt = preamble + body + closer
	}
	return finalPrompt
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
	// Extract the original user task so multi-turn tool continuing threads never lose context
	firstUser := ""
	for _, m := range messages {
		if strings.EqualFold(m.Role, "user") {
			c := strings.TrimSpace(m.Content)
			if c != "" && !strings.EqualFold(c, "(no content)") {
				firstUser = c
				break
			}
		}
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

	// Guarantee original task is preserved in continuing prompt when slice has no user turn
	if firstUser != "" {
		hasUserInSlice := false
		for _, m := range slice {
			if strings.EqualFold(m.Role, "user") {
				c := strings.TrimSpace(m.Content)
				if c != "" && !strings.EqualFold(c, "(no content)") {
					hasUserInSlice = true
					break
				}
			}
		}
		if !hasUserInSlice {
			slice = append([]types.ChatMessage{{Role: "user", Content: "[Task Goal]: " + firstUser}}, slice...)
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
				orig := strings.TrimSpace(m.Content)
				if orig != "" && !strings.EqualFold(orig, "(no content)") {
					c = reTotalTokens.ReplaceAllString(orig, "")
					c = reHookNotice.ReplaceAllString(c, "")
					c = strings.TrimSpace(c)
				}
				if c == "" {
					continue
				}
			}
			m.Content = c
		}
		out = append(out, m)
	}
	// If all user turns were dropped, retain at least one user turn to avoid sending empty prompt
	if len(out) == 0 && len(msgs) > 0 {
		for _, m := range msgs {
			if strings.EqualFold(m.Role, "user") && strings.TrimSpace(m.Content) != "" {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

var (
	reSysReminder    = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>\s*`)
	reTotalTokens    = regexp.MustCompile(`(?s)<total_tokens>.*?</total_tokens>\s*`)
	reScratchpadHint = regexp.MustCompile(`(?im)^First privately list what you need next;[^\n]*\n?`)
	reHookNotice     = regexp.MustCompile(`(?im)^(?:SessionStart|UserPromptSubmit)\b[^\n]*\n?`)
	reEnvContext     = regexp.MustCompile(`(?s)<environment_context>.*?</environment_context>\s*`)
	reLocalCaveat    = regexp.MustCompile(`(?s)<local-command-caveat>.*?</local-command-caveat>\s*`)
)

func stripWebUserNoise(s string) string {
	s = cleanSystemReminders(s)
	s = reTotalTokens.ReplaceAllString(s, "")
	s = reScratchpadHint.ReplaceAllString(s, "")
	s = reHookNotice.ReplaceAllString(s, "")
	s = reEnvContext.ReplaceAllString(s, "")
	s = reLocalCaveat.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func cleanSystemReminders(s string) string {
	return reSysReminder.ReplaceAllStringFunc(s, func(m string) string {
		trimmed := strings.TrimSpace(m)
		inner := strings.TrimPrefix(trimmed, "<system-reminder>")
		inner = strings.TrimSuffix(inner, "</system-reminder>")
		inner = reTotalTokens.ReplaceAllString(inner, "")
		inner = reHookNotice.ReplaceAllString(inner, "")
		inner = reScratchpadHint.ReplaceAllString(inner, "")
		inner = reLocalCaveat.ReplaceAllString(inner, "")
		inner = reEnvContext.ReplaceAllString(inner, "")
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return ""
		}
		// Strip "You are Claude Code" / harness identity to prevent prompt confusion
		if strings.Contains(inner, "You are Claude Code") {
			inner = strings.ReplaceAll(inner, "You are Claude Code", "")
			inner = strings.TrimSpace(inner)
		}
		// If outer text already has user instructions (e.g. text outside <system-reminder>),
		// and inner is just generic harness/skill listing, omit to save tokens.
		outer := strings.TrimSpace(reSysReminder.ReplaceAllString(s, ""))
		if outer != "" && !strings.EqualFold(outer, "(no content)") {
			low := strings.ToLower(inner)
			if !strings.Contains(low, "open-pr") && !strings.Contains(low, "<op>") && !strings.Contains(low, "review.md") {
				return ""
			}
		}
		if inner == "" {
			return ""
		}
		return "\n[System Context:\n" + inner + "\n]\n"
	})
}

func isClientHarness(s string) bool {
	if strings.Contains(s, "You are Claude Code") || strings.Contains(s, "x-anthropic-billing-header") {
		return true
	}
	if strings.Contains(s, "You are Antigravity") || (strings.Contains(s, "Antigravity") && strings.Contains(s, "agentic")) {
		return true
	}
	if strings.Contains(s, "You are Codex") || strings.Contains(s, "OpenAI Codex") || strings.Contains(s, "codex_cli") {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(s, "permission mode") && strings.Contains(low, "tool") {
		return true
	}
	if len([]rune(s)) > 2000 && (strings.Contains(low, "available tools") || strings.Contains(low, "input_schema") || strings.Contains(low, "functiondeclarations")) {
		return true
	}
	return false
}
