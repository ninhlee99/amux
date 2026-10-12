package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// Claude Code wraps background-agent events in metadata written for its own
// native model ("internal ID - do not mention to user", "Do NOT Read this
// file", "[SYSTEM NOTIFICATION - NOT USER INPUT]"). Pasted verbatim into a web
// chat those directives read like a prompt injection, and claude.ai refuses
// the whole task. Web backends get the same facts as one neutral line.

var (
	reAgentLaunchID  = regexp.MustCompile(`agentId:\s*([A-Za-z0-9_-]+)`)
	reTaskNotify     = regexp.MustCompile(`(?s)<task-notification>(.*?)</task-notification>`)
	reNotifyReminder = regexp.MustCompile(`(?s)<system-reminder>\s*(?:\[SYSTEM NOTIFICATION[^\]]*\][^<]*?)?(<task-notification>.*?</task-notification>)\s*</system-reminder>`)
	reNotifyPreface  = regexp.MustCompile(`(?s)\[SYSTEM NOTIFICATION - NOT USER INPUT\].*?(?:<task-notification>)`)
)

// SimplifyHarnessToolResult condenses Claude Code's "Async agent launched"
// tool result; any other content is returned unchanged.
func SimplifyHarnessToolResult(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "Async agent launched successfully") {
		return s
	}
	id := "unknown"
	if m := reAgentLaunchID.FindStringSubmatch(t); len(m) > 1 {
		id = m[1]
	}
	return fmt.Sprintf("Background subagent started (id %s). It is still running; its result will arrive later as a task notification.", id)
}

// SimplifyTaskNotifications rewrites each <task-notification> block in a user
// turn as a short "[Background task …]" report carrying its status, summary
// and result.
func SimplifyTaskNotifications(s string) string {
	if !strings.Contains(s, "<task-notification>") {
		return s
	}
	s = reNotifyReminder.ReplaceAllString(s, "$1")
	s = reNotifyPreface.ReplaceAllString(s, "<task-notification>")
	return reTaskNotify.ReplaceAllStringFunc(s, func(block string) string {
		body := reTaskNotify.FindStringSubmatch(block)[1]
		id, status := notifyField(body, "task-id"), notifyField(body, "status")
		var b strings.Builder
		fmt.Fprintf(&b, "[Background task %s %s]", id, status)
		if sum := notifyField(body, "summary"); sum != "" {
			b.WriteString(" " + sum)
		}
		if res := notifyField(body, "result"); res != "" {
			b.WriteString("\nResult:\n" + res)
		}
		return b.String()
	})
}

func notifyField(body, tag string) string {
	open, closeTag := "<"+tag+">", "</"+tag+">"
	i := strings.Index(body, open)
	if i < 0 {
		return ""
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, closeTag)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// relayDoubtStrong are phrases only a model doubting the relay itself uses;
// relayDoubtWeak also appear in ordinary answers ("I can't verify the
// version", "this file contains a prompt injection").
var (
	relayDoubtStrong = []string{
		"relayed text", "relayed into", "relayed from", "being relayed", "relayed conversation",
		"playing along", "no real tool execution", "not actual tool output", "not real tool",
		"simulated tool", "fabricated", "fake tool",
		"simulated environment", "tool mock", "mock calls", "another harness", "execution harness",
	}
	relayDoubtWeak = []string{
		"can't verify", "cannot verify", "can't confirm", "cannot confirm", "unable to verify",
		"injected", "prompt injection", "plain text, not",
		"does not have access", "don't have access", "do not have access", "not connected",
	}
)

// IsRelayDoubt reports a reply that declines to continue because the model
// doubts the relayed tool results: at least one relay-specific phrase plus a
// second signal, so a normal answer that mentions injection or uncertainty
// is not taken for it.
func IsRelayDoubt(text string) bool {
	low := strings.ToLower(strings.ReplaceAll(text, "’", "'"))
	if strings.Contains(low, "<tool_call>") {
		return false
	}
	strong, weak := 0, 0
	for _, s := range relayDoubtStrong {
		if strings.Contains(low, s) {
			strong++
		}
	}
	for _, s := range relayDoubtWeak {
		if strings.Contains(low, s) {
			weak++
		}
	}
	return strong >= 1 && strong+weak >= 2
}

// ResetsThread reports a reply after which the web conversation must not be
// reused: a tool refusal or relay doubt would steer every later turn.
func ResetsThread(text string) bool {
	return IsToolRefusal(text) || IsRelayDoubt(text)
}
