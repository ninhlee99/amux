package hook

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSyncClaudeSettingsEnv_SetThenUnset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Pre-existing settings with an unrelated env var must survive.
	m := map[string]any{"env": map[string]any{"FOO": "bar"}}
	if err := SaveClaudeSettings(m); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := SyncClaudeSettingsEnv(true, "http://127.0.0.1:8787"); err != nil {
		t.Fatalf("sync up: %v", err)
	}
	got := readSettingsEnv(t)
	if got["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" {
		t.Fatalf("expected BASE_URL set, got %v", got)
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "am-proxy" {
		t.Fatalf("expected AUTH_TOKEN set, got %v", got)
	}
	if got["FOO"] != "bar" {
		t.Fatalf("expected unrelated key preserved, got %v", got)
	}

	if err := SyncClaudeSettingsEnv(false, ""); err != nil {
		t.Fatalf("sync down: %v", err)
	}
	got = readSettingsEnv(t)
	if _, ok := got["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("expected BASE_URL removed, got %v", got)
	}
	if _, ok := got["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatalf("expected AUTH_TOKEN removed, got %v", got)
	}
	if got["FOO"] != "bar" {
		t.Fatalf("expected unrelated key preserved after unset, got %v", got)
	}
}

func TestSyncClaudeSettingsEnv_DownWithNoPriorEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SyncClaudeSettingsEnv(false, ""); err != nil {
		t.Fatalf("sync down on fresh settings: %v", err)
	}
	m := LoadClaudeSettings()
	if _, ok := m["env"]; ok {
		t.Fatalf("expected no env block written for a no-op unset, got %v", m)
	}
}

func readSettingsEnv(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	env, _ := m["env"].(map[string]any)
	if env == nil {
		return map[string]any{}
	}
	return env
}

func TestInstallStatusLine_DoesNotOverwriteCustom(t *testing.T) {
	m := map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "~/.claude/my-bar.sh"},
	}
	InstallStatusLine(m, `"/usr/bin/am" statusline`)
	sl := m["statusLine"].(map[string]any)
	if sl["command"] != "~/.claude/my-bar.sh" {
		t.Fatalf("must not overwrite custom statusline, got %v", sl["command"])
	}
}

func TestInstallStatusLine_ReplacesCaveman(t *testing.T) {
	m := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": `bash "/Users/x/.claude/hooks/caveman-statusline.sh"`,
		},
	}
	InstallStatusLine(m, `"/usr/local/bin/am" statusline`)
	if !IsOurStatusLine(m) {
		t.Fatal("caveman-only statusline must be replaced by am statusline")
	}
}

func TestInstallStatusLine_WritesOurs(t *testing.T) {
	m := map[string]any{}
	InstallStatusLine(m, `"/usr/local/bin/am" statusline`)
	if !IsOurStatusLine(m) {
		t.Fatal("expected our statusline")
	}
}
