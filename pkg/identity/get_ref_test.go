package identity

import "testing"

func TestGet_ByRowNumberAndProviderEmail(t *testing.T) {
	t.Setenv("AMUX_HOME", t.TempDir())
	cfg := &Config{Identities: []Identity{
		{ID: "codex:01", Provider: "codex", Tier: TierSubscription, Credentials: map[string]string{"account": "me@x.com"}},
		{ID: "claude:code:me", Provider: "claude", Tier: TierSubscription, Credentials: map[string]string{"account": "me@x.com"}},
	}}
	if err := SaveConfig("", cfg); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get("", "2"); got == nil || got.ID != "claude:code:me" {
		t.Fatalf("Get(2) = %+v", got)
	}
	if got, _ := Get("", "claude:me@x.com"); got == nil || got.ID != "claude:code:me" {
		t.Fatalf("Get(claude:me@x.com) = %+v", got)
	}
	if got, _ := Get("", "Codex:me@x.com"); got == nil || got.ID != "codex:01" {
		t.Fatalf("Get(Codex:me@x.com) = %+v", got)
	}
	if got, _ := Get("", "9"); got != nil {
		t.Fatalf("Get(9) = %+v, want nil", got)
	}
}

func TestDisplayProvider(t *testing.T) {
	cases := map[string]string{
		"codex:01":          "Codex",
		"chatgpt:01":        "ChatGPT Web",
		"claude:web:me":     "Claude Web",
		"claude:sub:01":     "Claude Code",
		"agy:01":            "Antigravity",
		"gemini:web:me":     "Gemini Web",
		"openrouter:api:01": "Openrouter",
	}
	for id, want := range cases {
		if got := (Identity{ID: id}).DisplayProvider(); got != want {
			t.Errorf("%s → %q, want %q", id, got, want)
		}
	}
}
