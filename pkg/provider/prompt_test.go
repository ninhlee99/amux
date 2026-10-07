package provider

import (
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestWebBackendPrompt_FullContextFlattensHistory(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "read foo.go"},
			{Role: "assistant", Content: "[Prior tool call: Read]\nfoo.go"},
			{Role: "user", Content: "fix the bug"},
		},
	}
	got := WebBackendPrompt(req, true) // continuingThread ignored when FullContext
	if !strings.Contains(got, "[xfer]") {
		t.Fatalf("missing handoff preamble: %q", got)
	}
	if !strings.Contains(got, "read foo.go") || !strings.Contains(got, "fix the bug") {
		t.Fatalf("missing turns: %q", got)
	}
	if !strings.Contains(got, "You are helpful.") {
		t.Fatalf("missing system: %q", got)
	}
	if strings.Contains(got, "If you cannot invoke tools") {
		t.Fatal("old handoff told the model to refuse tools")
	}
}

func TestWebBackendPrompt_ToolsCloserLast(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools:       []types.ToolDef{{Name: "Read"}, {Name: "Bash"}},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "Tools run behind a user-selected permission mode."},
			{Role: "user", Content: "please audit the readme file now"},
		},
	}
	got := WebBackendPrompt(req, false)
	closer := "[end]"
	if !strings.Contains(got, closer) {
		t.Fatal("missing closer")
	}
	if strings.LastIndex(got, closer) < strings.LastIndex(got, "please audit the readme file now") {
		t.Fatal("closer must follow user text")
	}
	if !strings.Contains(got, "permission mode") {
		t.Fatal("system content should be preserved")
	}
	if !strings.Contains(got, "<tool_call>") {
		t.Fatal("missing protocol")
	}
}

func TestWebBackendPrompt_ContinuingUsesLastUser(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "second"},
		},
	}
	got := WebBackendPrompt(req, true)
	if !strings.Contains(got, "sys") || !strings.Contains(got, "second") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "first") {
		t.Fatalf("should not flatten when continuing: %q", got)
	}
}

func TestWebBackendPrompt_LiveCatalogNoCodeChange(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools: []types.ToolDef{
			{Name: "mcp__new__search", InputSchema: []byte(`{"required":["q"],"properties":{"q":{"type":"string"}}}`)},
			{Name: "Skill", InputSchema: []byte(`{"required":["skill"],"properties":{"skill":{"type":"string"}}}`)},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are Claude Code, Anthropic's official CLI for Claude.\n" + strings.Repeat("x", 80)},
			{Role: "user", Content: "use the new mcp"},
		},
	}
	got := WebBackendPrompt(req, false)
	if !strings.Contains(got, "mcp__new__search:q:string") || !strings.Contains(got, "Skill:skill:string") {
		t.Fatalf("live catalog: %s", got)
	}
	if !strings.Contains(got, "You are Claude Code") {
		t.Fatal("harness content should be preserved")
	}
}

func TestWebBackendPrompt_ContinuingToolsPreservesFullContext(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools:       []types.ToolDef{{Name: "Read"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "old task about foo.go"},
			{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{Name: "Read", Arguments: `{"file_path":"foo.go"}`}}},
			{Role: "tool", Content: "package foo"},
			{Role: "user", Content: "now fix the bug"},
		},
	}
	got := WebBackendPrompt(req, true)
	if !strings.Contains(got, "old task about foo.go") {
		t.Fatalf("should keep full context: %q", got)
	}
	if !strings.Contains(got, "now fix the bug") || !strings.Contains(got, "package foo") {
		t.Fatalf("must keep latest: %q", got)
	}
	if !strings.Contains(got, "CATALOG") {
		t.Fatal("missing catalog")
	}
}

func TestWebBackendPrompt_PreservesSystemReminder(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools:       []types.ToolDef{{Name: "Read"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "<system-reminder>\nYou are Claude Code\n" + strings.Repeat("skill ", 40) + "\n</system-reminder>\n\nreview README.md"},
		},
	}
	got := WebBackendPrompt(req, false)
	if !strings.Contains(got, "system-reminder") || !strings.Contains(got, "You are Claude Code") {
		t.Fatal("reminder should be preserved", got)
	}
	if !strings.Contains(got, "review README.md") {
		t.Fatal("user task dropped", got)
	}
}

func TestWebBackendPrompt_NoToolsOmitsProtocol(t *testing.T) {
	got := WebBackendPrompt(&types.ChatRequest{
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	}, false)
	if strings.Contains(got, "<tool_call>") || strings.Contains(got, "CATALOG") {
		t.Fatal("no tools[] must not inject web protocol", got)
	}
}

func TestWebBackendPrompt_PreservesOpenPRSkillReminder(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools:       []types.ToolDef{{Name: "Bash"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "<system-reminder>\n<total_tokens>15000000 tokens left</total_tokens>\n- open-pr:review: Review PRs against conventions\ncall fetches everything — `<op> context`\n</system-reminder>\n\n(no content)"},
		},
	}
	got := WebBackendPrompt(req, false)
	if !strings.Contains(got, "open-pr:review") || !strings.Contains(got, "<op> context") {
		t.Fatal("open-pr skill instructions must be preserved in web prompt", got)
	}
}

func TestWebBackendPrompt_ContinuingThreadPreservesTaskGoal(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: true,
		Tools:       []types.ToolDef{{Name: "Bash"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "/open-pr:review https://github.com/ninhlee99/amux/pull/39"},
			{Role: "assistant", ToolCalls: []types.ToolCall{{Name: "Bash", Arguments: `{"command":"git status -sb"}`}}},
			{Role: "tool", ToolCallID: "toolu_web_1", Content: "## feat/amux-merged-latest...origin/feat/amux-merged-latest"},
		},
	}
	got := WebBackendPrompt(req, true)
	if !strings.Contains(got, "open-pr:review") || !strings.Contains(got, "pull/39") {
		t.Fatalf("continuing thread must preserve initial task goal so model knows what to do, got: %s", got)
	}
}

func TestWebBackendPrompt_ContinuingThreadWithoutFullContextPreservesToolResults(t *testing.T) {
	req := &types.ChatRequest{
		FullContext: false,
		Tools:       []types.ToolDef{{Name: "Bash"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "check directory structure"},
			{Role: "assistant", ToolCalls: []types.ToolCall{{Name: "Bash", Arguments: `{"command":"ls -la"}`}}},
			{Role: "tool", ToolCallID: "toolu_web_1", Content: "total 40\ndrwxr-xr-x 5 staff 160 main.go"},
		},
	}
	got := WebBackendPrompt(req, true)
	if !strings.Contains(got, "main.go") {
		t.Fatalf("continuing thread without FullContext must preserve tool results, got: %s", got)
	}
	if !strings.Contains(got, "check directory structure") {
		t.Fatalf("continuing thread must preserve user task, got: %s", got)
	}
}



// A fresh web thread given a transcript that already holds tool turns must be
// told to continue the task; without the cue ChatGPT answers "Ready. Send the
// repo task" and the client's tool loop stalls.
func TestWebBackendPrompt_FreshThreadWithToolHistoryAsksForNextTurn(t *testing.T) {
	req := &types.ChatRequest{
		Tools: []types.ToolDef{{Name: "Bash"}},
		Messages: []types.ChatMessage{
			{Role: "user", Content: "run pwd then ls go.mod"},
			{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "toolu_a", Name: "Bash", Arguments: `{"command":"pwd"}`}}},
			{Role: "tool", ToolCallID: "toolu_a", Content: "/repo"},
		},
	}
	got := WebBackendPrompt(req, false)
	cue := strings.LastIndex(got, "[next]")
	if cue < 0 || cue < strings.LastIndex(got, "/repo") {
		t.Fatalf("missing next-turn cue after transcript: %q", got)
	}
	if !strings.Contains(got[cue:], "Task: run pwd then ls go.mod") {
		t.Fatalf("cue must restate the task: %q", got[cue:])
	}

	noTools := WebBackendPrompt(&types.ChatRequest{
		Tools:    req.Tools,
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	}, false)
	if strings.Contains(noTools, "[next]") {
		t.Fatalf("cue only belongs after tool turns: %q", noTools)
	}
}
