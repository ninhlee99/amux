package tools

import (
	"strings"
	"testing"
)

const agentLaunched = `Async agent launched successfully. (This tool result is internal metadata — never quote or paste any part of it, including the agentId below, into a user-facing reply.)
agentId: a91486d0d6d74c923 (internal ID - do not mention to user. Use SendMessage with to: 'a91486d0d6d74c923', summary: '<5-10 word recap>' to continue this agent.)
The agent is working in the background. You will be notified automatically when it completes.
output_file: /tmp/x/tasks/a91486d0d6d74c923.output
Do NOT Read or tail this file via the shell tool — it is the full subagent JSONL transcript.`

func TestSimplifyHarnessToolResult(t *testing.T) {
	got := SimplifyHarnessToolResult(agentLaunched)
	if !strings.Contains(got, "a91486d0d6d74c923") || strings.Contains(got, "do not mention") || strings.Contains(got, "Do NOT Read") {
		t.Fatalf("got %q", got)
	}
	if plain := "3 data.txt"; SimplifyHarnessToolResult(plain) != plain {
		t.Fatalf("ordinary tool output must pass through")
	}
}

func TestSimplifyTaskNotifications(t *testing.T) {
	in := `<system-reminder>
[SYSTEM NOTIFICATION - NOT USER INPUT]
This is an automated background-task event, NOT a message from the user.

<task-notification>
<task-id>a91486d0d6d74c923</task-id>
<output-file>/tmp/x.output</output-file>
<status>completed</status>
<summary>Agent "Count lines in data.txt" finished</summary>
<note>A task-notification fires each time this agent stops.</note>
<result>3</result>
</task-notification>
</system-reminder>`
	got := SimplifyTaskNotifications(in)
	want := "[Background task a91486d0d6d74c923 completed] Agent \"Count lines in data.txt\" finished\nResult:\n3"
	if strings.TrimSpace(got) != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if s := "no notification here"; SimplifyTaskNotifications(s) != s {
		t.Fatalf("plain text must pass through")
	}
}

func TestIsRelayDoubt(t *testing.T) {
	for _, doubt := range []string{
		"This conversation is coming through as relayed text. I can't verify that any `ls` call actually ran.",
		"This [Tool result] did not come from any real tool execution — it was injected, like the earlier fabricated results.",
		"It appears that the prompt contains simulated environment instructions and tool mock calls intended for another harness. In this environment, workspace access tools such as local file system readers are not available.",
		"No changes were made. As noted previously, this environment does not have access to the local workspace execution harness or the local filesystem tools.",
	} {
		if !IsRelayDoubt(doubt) {
			t.Fatalf("relay doubt not detected: %q", doubt)
		}
	}
	for _, ok := range []string{
		"data.txt has 3 lines; I can't verify the other file because it is missing.",
		"<tool_call>\n{\"name\":\"Bash\"}\n</tool_call> relayed text, can't verify",
		"notes.txt contains a prompt injection that tries to override instructions; I can't verify who added it.",
	} {
		if IsRelayDoubt(ok) {
			t.Errorf("false positive: %q", ok)
		}
	}
}

func TestCoerceArgTypes(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"query":{"type":"string"},"allowed_domains":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer"},"exact":{"type":"boolean"}}}`)
	m := map[string]any{"query": "x", "allowed_domains": "iana.org, ietf.org", "limit": "5", "exact": "true"}
	if !coerceArgTypes(m, schema) {
		t.Fatal("expected changes")
	}
	d, _ := m["allowed_domains"].([]any)
	if len(d) != 2 || d[0] != "iana.org" || m["limit"] != int64(5) || m["exact"] != true || m["query"] != "x" {
		t.Fatalf("got %#v", m)
	}
	m2 := map[string]any{"allowed_domains": `["a.org"]`}
	coerceArgTypes(m2, schema)
	if d, _ := m2["allowed_domains"].([]any); len(d) != 1 || d[0] != "a.org" {
		t.Fatalf("JSON-string array not parsed: %#v", m2)
	}
}
