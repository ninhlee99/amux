package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustTarget(t *testing.T, name string) Target {
	t.Helper()
	tg, ok := FindTarget(name)
	if !ok {
		t.Fatalf("no target %s", name)
	}
	return tg
}

func TestInstallJSONPreservesOtherServers(t *testing.T) {
	home := t.TempDir()
	tg := mustTarget(t, "cursor")
	path := tg.Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := `{"mcpServers":{"other":{"command":"x"}},"keep":true}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Install(home, "/opt/amux"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	if servers["other"] == nil || doc["keep"] != true {
		t.Fatalf("lost existing config: %s", b)
	}
	entry := servers["amux"].(map[string]any)
	if entry["command"] != "/opt/amux" || entry["args"].([]any)[0] != "mcp" {
		t.Fatalf("entry = %v", entry)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Fatalf("permissions changed to %v", st.Mode().Perm())
	}
	if bak, _ := os.ReadFile(path + ".amux.bak"); string(bak) != orig {
		t.Fatalf("backup = %q", bak)
	}
	if !tg.Installed(home) {
		t.Fatal("Installed() = false after install")
	}
	// Idempotent.
	if _, err := tg.Install(home, "/opt/amux"); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Uninstall(home); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "/opt/amux") || !strings.Contains(string(b), "other") {
		t.Fatalf("uninstall result: %s", b)
	}
}

func TestInstallCreatesMissingFile(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"vscode", "opencode", "gemini", "agy", "windsurf", "claude-desktop", "cline", "roo"} {
		tg := mustTarget(t, name)
		if _, err := tg.Install(home, "/opt/amux"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !tg.Installed(home) {
			t.Fatalf("%s: not installed", name)
		}
	}
	b, _ := os.ReadFile(mustTarget(t, "opencode").Path(home))
	if !strings.Contains(string(b), `"type": "local"`) || !strings.Contains(string(b), `"mcp"`) {
		t.Fatalf("opencode entry: %s", b)
	}
	b, _ = os.ReadFile(mustTarget(t, "vscode").Path(home))
	if !strings.Contains(string(b), `"servers"`) {
		t.Fatalf("vscode entry: %s", b)
	}
}

func TestInstallRefusesJSONC(t *testing.T) {
	home := t.TempDir()
	tg := mustTarget(t, "zed")
	path := tg.Path(home)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	orig := "// my settings\n{\"theme\": \"One Dark\"}\n"
	_ = os.WriteFile(path, []byte(orig), 0o600)
	if _, err := tg.Install(home, "/opt/amux"); err != ErrHasComments {
		t.Fatalf("err = %v, want ErrHasComments", err)
	}
	if b, _ := os.ReadFile(path); string(b) != orig {
		t.Fatalf("file was modified: %q", b)
	}
	if !strings.Contains(tg.Snippet("/opt/amux"), "context_servers") {
		t.Fatal("snippet should use zed's context_servers key")
	}
}

func TestCodexTomlBlock(t *testing.T) {
	home := t.TempDir()
	tg := mustTarget(t, "codex")
	path := tg.Path(home)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	orig := "model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"x\"\n"
	_ = os.WriteFile(path, []byte(orig), 0o600)
	for i := 0; i < 2; i++ { // idempotent
		if _, err := tg.Install(home, `/Users/me/bin/amux`); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Count(s, "[mcp_servers.amux]") != 1 || !strings.Contains(s, `command = "/Users/me/bin/amux"`) ||
		!strings.Contains(s, "[mcp_servers.other]") || !strings.Contains(s, `model = "gpt-5"`) {
		t.Fatalf("config.toml:\n%s", s)
	}
	if _, err := tg.Uninstall(home); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "amux") || !strings.Contains(string(b), "[mcp_servers.other]") {
		t.Fatalf("after uninstall:\n%s", b)
	}
}

func TestSetCodexBlockRemovesSubtables(t *testing.T) {
	in := "[mcp_servers.amux]\ncommand = \"a\"\n[mcp_servers.amux.env]\nX = \"1\"\n[profile]\nname = \"p\"\n"
	out := setCodexBlock(in, "")
	if strings.Contains(out, "amux") || !strings.Contains(out, "[profile]") {
		t.Fatalf("out = %q", out)
	}
}

func TestStripJSONComments(t *testing.T) {
	in := `{"url": "http://x//y", /* c */ "a": 1 // tail
}`
	var v map[string]any
	if err := json.Unmarshal(stripJSONComments([]byte(in)), &v); err != nil {
		t.Fatal(err)
	}
	if v["url"] != "http://x//y" || v["a"].(float64) != 1 {
		t.Fatalf("v = %v", v)
	}
}

func TestFindTargetAliases(t *testing.T) {
	for alias, want := range map[string]string{"claude-code": "claude", "antigravity": "agy", "copilot": "vscode", "Cursor": "cursor"} {
		if tg, ok := FindTarget(alias); !ok || tg.Name != want {
			t.Errorf("FindTarget(%q) = %v, %v", alias, tg.Name, ok)
		}
	}
}

func TestUninstallLeavesNothingBehind(t *testing.T) {
	home := t.TempDir()
	tg := mustTarget(t, "cursor")
	path := tg.Path(home)

	// File amux created: gone after uninstall, no backup left.
	if _, err := tg.Install(home, "/opt/amux"); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Uninstall(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config amux created should be removed, stat err=%v", err)
	}

	// Pre-existing file: restored to the user's content, backup removed.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := `{"mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Install(home, "/opt/amux"); err != nil {
		t.Fatal(err)
	}
	if _, err := tg.Uninstall(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".amux.bak"); !os.IsNotExist(err) {
		t.Fatal("uninstall must remove the .amux.bak backup")
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers["other"] == nil || servers[ServerName] != nil {
		t.Fatalf("after uninstall: %s", b)
	}
	if tg.Installed(home) {
		t.Fatal("still reported as installed")
	}
}
