package router

import (
	"reflect"
	"testing"

	"amux-accounts/pkg/types"
)

func TestSoftPreferGroupsNeverDrops(t *testing.T) {
	base := []string{GroupCodexSub, GroupAPIOther, GroupClaudeWeb, GroupChatGPTWeb}
	prefer := PreferredGroupsForTask(TaskAnalysis)
	got := SoftPreferGroups(base, prefer)
	if len(got) != len(base) {
		t.Fatalf("dropped groups: got %v base %v", got, base)
	}
	if got[0] != GroupClaudeWeb {
		t.Fatalf("analysis should prefer claude web first among base, got %v", got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, g := range base {
		if !seen[g] {
			t.Fatalf("missing %s in %v", g, got)
		}
	}
}

func TestPreferredGroupsForTaskCodingHierarchy(t *testing.T) {
	pref := PreferredGroupsForTask(TaskCoding)
	if pref[0] != GroupClaudeSub {
		t.Fatalf("coding prefer claude sub first, got %v", pref)
	}
	if pref[1] != GroupCodexSub {
		t.Fatalf("coding prefer codex sub second, got %v", pref)
	}
}

func TestPreferredGroupsForWebTasks(t *testing.T) {
	for _, kind := range []string{TaskPlan, TaskClarify, TaskAnalysis, TaskReview, TaskCompact, TaskQuality} {
		if !IsWebTask(kind) {
			t.Errorf("expected IsWebTask(%q) to be true", kind)
		}
		pref := PreferredGroupsForTask(kind)
		if len(pref) < 3 || pref[0] != GroupClaudeWeb {
			t.Errorf("task %q should prefer claude web first, got %v", kind, pref)
		}
	}
}

func TestGroupOrderForRequestAnalysis(t *testing.T) {
	req := &types.ChatRequest{ClientDialect: "claude", TaskKind: TaskAnalysis}
	got := GroupOrderForRequest(req)
	// Claude IDE base skips claude_sub; analysis soft-prefers web first.
	if got[0] != GroupClaudeWeb {
		t.Fatalf("want claude web first for analysis, got %v", got)
	}
	for _, g := range got {
		if IsClaudeSubscriptionGroup(g) {
			t.Fatalf("must not include claude sub: %v", got)
		}
	}
}

func TestClassifyTaskKind(t *testing.T) {
	cases := []struct {
		name string
		req  *types.ChatRequest
		kind string
	}{
		{
			name: "plan vietnamese",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Hãy lên kế hoạch refactor hệ thống sang microservices"},
			}},
			kind: TaskPlan,
		},
		{
			name: "clarify vietnamese",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Làm rõ yêu cầu về tính năng thanh toán giúp tôi"},
			}},
			kind: TaskClarify,
		},
		{
			name: "plan english",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Please outline an implementation plan for OAuth migration"},
			}},
			kind: TaskPlan,
		},
		{
			name: "clarify english",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Can you clarify requirements for the search filter?"},
			}},
			kind: TaskClarify,
		},
		{
			name: "analysis vietnamese",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Hãy phân tích kiến trúc module router giúp tôi"},
			}},
			kind: TaskAnalysis,
		},
		{
			name: "review vietnamese",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Hãy review code và đánh giá pull request này"},
			}},
			kind: TaskReview,
		},
		{
			name: "review with mutating tools in catalog",
			req: &types.ChatRequest{
				Tools:    []types.ToolDef{{Name: "Bash"}, {Name: "Edit"}, {Name: "Write"}},
				Messages: []types.ChatMessage{{Role: "user", Content: "kiểm tra code giúp tôi và soi xét kỹ logic"}},
			},
			kind: TaskReview,
		},
		{
			name: "danh gia lai chat luong pr",
			req: &types.ChatRequest{
				Tools:    []types.ToolDef{{Name: "Bash"}, {Name: "Edit"}},
				Messages: []types.ChatMessage{{Role: "user", Content: "đánh giá lại chất lượng và logic của pull request này"}},
			},
			kind: TaskReview,
		},
		{
			name: "coding with mutating tool",
			req: &types.ChatRequest{
				Tools:    []types.ToolDef{{Name: "Bash"}, {Name: "Edit"}},
				Messages: []types.ChatMessage{{Role: "user", Content: "implement login endpoint"}},
			},
			kind: TaskCoding,
		},
		{
			name: "fix bug",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Please fix bug in auth middleware"},
			}},
			kind: TaskFix,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyTask(tc.req).Kind
			if got != tc.kind {
				t.Fatalf("kind=%q want %q", got, tc.kind)
			}
		})
	}
}

func TestSoftPreferGroupsEmptyPrefer(t *testing.T) {
	base := []string{GroupCodexSub, GroupAPIOther}
	got := SoftPreferGroups(base, nil)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("got %v want %v", got, base)
	}
}
