package tools_test

import (
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestWebloop_OpenPRReviewRefusal_NoForcedHallucination(t *testing.T) {
	refusalText := `I can’t complete the /open-pr:review run in this chat because the required open-pr runtime is not available to me here.
Alternatively, provide the PR diff/context here and I can do a normal read-only code review without posting to GitHub.`

	defs := []types.ToolDef{
		{Name: "Bash"},
		{Name: "Read"},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review please review PR #42"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if forced || len(calls) != 0 {
		t.Fatalf("expected no forced tool calls on natural language refusal, got forced=%v, calls=%d (%+v)", forced, len(calls), calls)
	}
}

func TestWebloop_PRReviewNumberRefusal_NoForcedHallucination(t *testing.T) {
	refusalText := `Mình thấy tin nhắn vừa rồi chỉ chứa phần CATALOG/tool schema và không có nội dung PR hoặc diff để review.
Nếu bạn muốn mình tiếp tục review PR #39, vui lòng gửi lại link PR.`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "please review https://github.com/ninhlee99/amux/pull/39"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if forced || len(calls) != 0 {
		t.Fatalf("expected no forced tool calls on plain text refusal, got forced=%v, calls=%d", forced, len(calls))
	}
}

func TestWebloop_ExplicitToolCall_ParsedCorrectly(t *testing.T) {
	text := `Sure, let me check the git status first:
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	calls, forced := tools.FinalizeWebToolCalls(text, defs, nil)
	if forced {
		t.Fatalf("explicit tool call should not be marked forced")
	}
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash tool call, got: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status argument, got: %s", calls[0].Arguments)
	}
}
