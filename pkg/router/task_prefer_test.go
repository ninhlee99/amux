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

func TestPreferredGroupsForTaskCodingPrefersAPI(t *testing.T) {
	pref := PreferredGroupsForTask(TaskCoding)
	if pref[0] != GroupAPIOther {
		t.Fatalf("coding prefer api first, got %v", pref)
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
			name: "analysis vietnamese",
			req: &types.ChatRequest{Messages: []types.ChatMessage{
				{Role: "user", Content: "Hãy phân tích kiến trúc module router giúp tôi"},
			}},
			kind: TaskAnalysis,
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
