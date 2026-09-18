package tools_test

import (
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
