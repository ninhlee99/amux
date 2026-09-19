package tools_test

import (
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestWebloop_OpenPRReviewRefusal_Interception(t *testing.T) {
	refusalText := `I can’t complete the /open-pr:review run in this chat because the required open-pr runtime (<op>, repository worktree checkout, PR context fetch, posting review, etc.) is not available to me here.

The previous attempt also could not access the required plugin files (core/guardrails.md, core/cli.md) from the specified cache path, so I don’t have the execution context needed to safely perform the review flow or post a PR review.

If you run this in the environment where the open-pr plugin is installed, it should be able to execute the review steps. Alternatively, provide the PR diff/context here and I can do a normal read-only code review without posting to GitHub.`

	defs := []types.ToolDef{
		{Name: "Bash"},
		{Name: "Read"},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review please review PR #42"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) == 0 {
		t.Fatalf("expected forced tool exploration on open-pr refusal, got forced=%v, calls=%d", forced, len(calls))
	}

	hasPRCommand := false
	for _, c := range calls {
		if c.Name == "Bash" {
			hasPRCommand = true
			break
		}
	}
	if !hasPRCommand {
		t.Fatalf("expected Bash tool to fetch PR diff/context, got: %+v", calls)
	}
}

func TestWebloop_PRReviewNumberRefusal_Interception(t *testing.T) {
	refusalText := `Mình thấy tin nhắn vừa rồi chỉ chứa phần CATALOG/tool schema và không có nội dung PR hoặc diff để review.

Nếu bạn muốn mình tiếp tục review PR #39, vui lòng gửi lại:
- link PR: https://github.com/ninhlee99/amux/pull/39 (nếu môi trường có quyền truy cập), hoặc
- output của: git status
kèm phần output.
Mình sẽ review trực tiếp trên diff và trả về các finding cụ thể (nếu có).`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review https://github.com/ninhlee99/amux/pull/39"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) == 0 {
		t.Fatalf("expected forced tool exploration, got forced=%v, calls=%d", forced, len(calls))
	}

	hasPRDiff := false
	for _, c := range calls {
		if c.Name == "Bash" && (strings.Contains(c.Arguments, "open-pr.sh context") || strings.Contains(c.Arguments, "gh pr diff 39") || strings.Contains(c.Arguments, "git diff main...HEAD")) {
			hasPRDiff = true
			break
		}
	}
	if !hasPRDiff {
		t.Fatalf("expected PR context or diff fetching with PR 39, got: %+v", calls)
	}
}

func TestWebloop_TruncatedDiffRefusal_TargetedDiff(t *testing.T) {
	refusalText := `I can’t produce a reliable PR review from the available context: the provided diff output is truncated (only the README portion and PR metadata are visible), and the repository inspection commands did not return usable file contents in this session.

A production-grade review requires the actual changed hunks, especially for the modified Go files:
- pkg/provider/chatgpt_web.go
- pkg/provider/claude_web.go
- pkg/runtime/contract.go

With the complete diff available, I’ll review for:
- 🔴 MUST FIX
- 🟠 SHOULD FIX
- 🔵 SUGGESTION`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	// History already has gh pr diff
	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review https://github.com/ninhlee99/amux/pull/39"},
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{Name: "Bash", Arguments: `{"command":"gh pr diff 39"}`},
			},
		},
		{
			Role:    "tool",
			Content: "<persisted-output>\nOutput too large (222.3KB). Full output saved to: /tmp/output.txt\nPreview:\ndiff --git a/README.md",
		},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) == 0 {
		t.Fatalf("expected forced tool exploration on truncated diff refusal, got forced=%v, calls=%d", forced, len(calls))
	}

	hasTargetedDiff := false
	for _, c := range calls {
		if c.Name == "Bash" && strings.Contains(c.Arguments, "git diff") && strings.Contains(c.Arguments, "pkg/provider/chatgpt_web.go") {
			hasTargetedDiff = true
			break
		}
	}
	if !hasTargetedDiff {
		t.Fatalf("expected targeted git diff for candidate files, got calls: %+v", calls)
	}
}

