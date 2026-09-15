package ui

import (
	"testing"

	"amux-accounts/pkg/router"
)

func TestSortAccountRowsByGroup(t *testing.T) {
	rows := []accountRow{
		{Group: router.GroupAPIOther, ID: "gemini:api:01", Priority: 1},
		{Group: router.GroupCodexSub, ID: "codex:02", Priority: 2},
		{Group: router.GroupCodexSub, ID: "codex:01", Priority: 1},
		{Group: router.GroupClaudeSub, ID: "ninhle", Priority: 0},
		{Group: router.GroupChatGPTWeb, ID: "chatgpt:01", Priority: 8},
		{Group: router.GroupAPIOther, ID: "groq:api:01", Priority: 3},
	}
	sortAccountRowsByGroup(rows)
	want := []string{"ninhle", "codex:01", "codex:02", "gemini:api:01", "groq:api:01", "chatgpt:01"}
	for i, id := range want {
		if rows[i].ID != id {
			t.Fatalf("pos %d: got %s want %s (full=%v)", i, rows[i].ID, id, idsOf(rows))
		}
	}
}

func TestForEachAccountGroupSplitsAPIByProvider(t *testing.T) {
	rows := []accountRow{
		{Group: router.GroupGeminiWeb, ID: "g"},
		{Group: router.GroupCodexSub, ID: "c"},
		{Group: router.GroupAPIOther, ID: "groq:api:01", Kind: "api"},
		{Group: router.GroupAPIOther, ID: "gemini:api:02", Kind: "gemini-api"},
		{Group: router.GroupAPIOther, ID: "gemini:api:01", Kind: "gemini-api"},
	}
	var titles []string
	var counts []int
	forEachAccountGroup(rows, func(title, group string, members []accountRow) {
		titles = append(titles, title)
		counts = append(counts, len(members))
		_ = group
	})
	wantTitles := []string{
		"Codex Subscription",
		"API · GEMINI",
		"API · GROQ",
		"Gemini Web",
	}
	if len(titles) != len(wantTitles) {
		t.Fatalf("titles=%v want %v", titles, wantTitles)
	}
	for i := range wantTitles {
		if titles[i] != wantTitles[i] {
			t.Fatalf("titles=%v want %v", titles, wantTitles)
		}
	}
	if counts[1] != 2 || counts[2] != 1 {
		t.Fatalf("api counts=%v", counts)
	}
}

func TestAPIProviderBrand(t *testing.T) {
	cases := map[string]string{
		"gemini:api:01":     "gemini",
		"groq:api:01":       "groq",
		"openrouter:api:01": "openrouter",
		"github:api:01":     "github",
		"kimi:api:01":       "kimi",
		"grok:api:01":       "grok",
	}
	for id, want := range cases {
		if got := apiProviderBrand(id, "api"); got != want {
			t.Errorf("%s: got %s want %s", id, got, want)
		}
	}
}

func TestMatchesAccountRowFilter(t *testing.T) {
	gemini := withAPIProvider(accountRow{Group: router.GroupAPIOther, ID: "gemini:api:01", Kind: "gemini-api"})
	claude := accountRow{Group: router.GroupClaudeSub, ID: "ninhle", Kind: "claude"}
	web := accountRow{Group: router.GroupClaudeWeb, ID: "claude:web:01", Kind: "claude-web"}

	if !matchesAccountRowFilter("gemini", gemini) {
		t.Fatal("gemini filter should match gemini:api")
	}
	if matchesAccountRowFilter("gemini", claude) {
		t.Fatal("gemini filter must not match claude sub")
	}
	if !matchesAccountRowFilter("api", gemini) {
		t.Fatal("api filter")
	}
	if !matchesAccountRowFilter("claude", claude) || !matchesAccountRowFilter("claude", web) {
		t.Fatal("claude filter covers sub + web")
	}
	if !matchesAccountRowFilter("web", web) {
		t.Fatal("web filter")
	}
}

func idsOf(rows []accountRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
