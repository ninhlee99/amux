package provider

import (
	"regexp"
	"strings"

	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

const contextHandoffPreamble = `[xfer] Continue. [Tool result] = real CLI output. Emit <tool_call> if you need files/commands; else answer.

`

// WebBackendPrompt builds the single string web UIs accept.
//
// A live thread (continuingThread without FullContext) already holds the
// client's system prompt and earlier turns, so it gets only what it has not
// seen: the new user request, or the tool results since the last reply.
// A fresh thread or a handoff gets the full transcript, shrunk to the web
// token budget (~20k tokens) while keeping the system prompt, first user
// goal and recent tail.
func WebBackendPrompt(req *types.ChatRequest, continuingThread bool) string {
	if req == nil {
		return ""
	}
	msgs := req.Messages
	// Decide on the client's history, not a shrunk copy: Claude Code's
	// system prompt alone exceeds the web budget, so shrinking folds the tool
	// turns into a [compact] note and the session would look tool-free.
	ranTools := historyHasToolTurns(msgs)
	liveThread := continuingThread && !req.FullContext

	var body string
	if liveThread {
		if ranTools {
			body = BuildDeltaWebPrompt(msgs)
		} else {
			body = lastUserPrompt(msgs)
		}
	} else {
		if len(req.Tools) > 0 && ctxshrink.EstimateMessagesTokens(msgs) > ctxshrink.DefaultWebMaxTokens {
			msgs = ctxshrink.FitMessagesToTokenBudget(msgs, ctxshrink.DefaultWebMaxTokens)
		}
		body = BuildConcatenatedPrompt(msgs)
		if req.FullContext {
			if body == "" {
				return ""
			}
			body = contextHandoffPreamble + body
		}
	}

	if len(req.Tools) == 0 {
		return enforceWebPromptLimit(body, ctxshrink.AbsoluteMaxWebRunes)
	}
	closer := tools.WebCloser()
	if !liveThread {
		if ranTools {
			closer = midTaskCue(req.Messages) + closer
		}
	} else if ranTools && endsWithUserRequest(req.Messages) {
		closer = newRequestCue + closer
	}
	var preamble string
	if continuingThread {
		preamble = tools.WebCatalogOnly(req.Tools)
	} else {
		preamble = tools.WebPreambleForRequest(req)
	}
	trimmedBody := strings.TrimSpace(body)
	var finalPrompt string
	if strings.HasSuffix(trimmedBody, "Assistant:") {
		trimmedBody = strings.TrimSuffix(trimmedBody, "Assistant:")
		body = strings.TrimSpace(trimmedBody) + "\n\n" + strings.TrimSpace(closer) + "\n\nAssistant: "
		finalPrompt = preamble + body
	} else {
		finalPrompt = preamble + body + closer
	}
	return enforceWebPromptLimit(finalPrompt, ctxshrink.AbsoluteMaxWebRunes)
}

func enforceWebPromptLimit(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	head := maxRunes * 4 / 10
	tail := maxRunes * 4 / 10
	return string(r[:head]) + "\n\n... [history truncated to fit web payload limit] ...\n\n" + string(r[len(r)-tail:])
}

// midTaskCue follows a full transcript that already holds tool turns. Sent on
// a fresh web thread, such a transcript reads to ChatGPT like setup text and it
// replies "Ready. Send the task" instead of continuing; naming the task and
// asking for the next turn keeps it working.
func midTaskCue(msgs []types.ChatMessage) string {
	cue := "\n[next] You are the Assistant above, mid-task."
	if task := currentUserTask(msgs); task != "" {
		cue += " Task: " + truncateRunes(task, 500)
	}
	return cue + "\nWrite your next turn now: the next <tool_call>, or the final answer if the [Tool result]s suffice. Do not wait for a new task.\n"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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

// BuildDeltaWebPrompt sends only what the live web thread has not seen: the
// turns after the last assistant reply. A text-only assistant reply is
// already in the thread, so it is not replayed when a new user request
// follows it — replaying it reads to ChatGPT like a pasted transcript, and it
// answers "I have no access to the repo" instead of acting.
func BuildDeltaWebPrompt(messages []types.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}

	// The delta turns since the last assistant response are all subsequent
	// tool results and/or new user messages.
	lastAssistant := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			lastAssistant = i
			break
		}
	}

	var slice []types.ChatMessage
	switch {
	case lastAssistant < 0:
		slice = messages
	case len(messages[lastAssistant].ToolCalls) == 0 && hasUserTurn(messages[lastAssistant+1:]):
		slice = messages[lastAssistant+1:]
	default:
		slice = messages[lastAssistant:]
	}

	// Mid tool loop the delta holds only tool results: restate the request
	// that started this loop (not the session's first one) so the model
	// keeps working on the current task.
	if !hasUserTurn(slice) {
		if task := currentUserTask(messages); task != "" {
			slice = append([]types.ChatMessage{{Role: "user", Content: "[Task Goal]: " + task}}, slice...)
		}
	}

	return BuildConcatenatedPrompt(slice)
}

// hasUserTurn reports whether msgs holds a non-empty user turn.
func hasUserTurn(msgs []types.ChatMessage) bool {
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "user") {
			c := strings.TrimSpace(m.Content)
			if c != "" && !strings.EqualFold(c, "(no content)") {
				return true
			}
		}
	}
	return false
}

// currentUserTask is the latest non-empty user turn: the request the client
// is working on now. Later requests in a session supersede the first one.
func currentUserTask(messages []types.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if strings.EqualFold(m.Role, "user") {
			c := strings.TrimSpace(m.Content)
			if c != "" && !strings.EqualFold(c, "(no content)") {
				return c
			}
		}
	}
	return ""
}

// newRequestCue follows a new user request on a live thread whose session
// already ran tools. After a long text answer ChatGPT tends to treat a
// follow-up like "now implement it" as chat and claims it cannot reach the
// repo; this points it back at <tool_call>.
const newRequestCue = "\n[next] New request from the user above. If it needs the repo (read, edit, run, implement), start with <tool_call> now — do not just describe a plan or say you lack access.\n"

// endsWithUserRequest is true when the last turn is a user message, not a tool result.
func endsWithUserRequest(msgs []types.ChatMessage) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	c := strings.TrimSpace(last.Content)
	return strings.EqualFold(last.Role, "user") && c != "" && !strings.EqualFold(c, "(no content)")
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
		// and inner is an oversized listing, omit to preserve tokens.
		outer := strings.TrimSpace(reSysReminder.ReplaceAllString(s, ""))
		if outer != "" && !strings.EqualFold(outer, "(no content)") {
			if len(inner) > 1000 {
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
	if strings.Contains(s, "You are Cursor") || strings.Contains(s, "Cursor AI") {
		return true
	}
	if strings.Contains(s, "You are Cline") || strings.Contains(s, "Cline, a helpful") {
		return true
	}
	if strings.Contains(s, "You are Roo") || strings.Contains(s, "Roo Code") {
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
