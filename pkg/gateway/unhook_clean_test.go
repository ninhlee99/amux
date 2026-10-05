package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnhookAgy_KeepsUserModelProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_HOME", filepath.Join(home, ".amux"))
	p := AGYSettingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"modelProvider":"gemini","theme":"dark"}`), 0o600)

	if err := HookAgy(""); err != nil {
		t.Fatal(err)
	}
	if err := UnhookAgy(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(p)
	_ = json.Unmarshal(b, &m)
	if m["modelProvider"] != "gemini" || m["theme"] != "dark" || m["env"] != nil {
		t.Fatalf("unhook changed user settings: %s", b)
	}
}

func TestUnhookAgy_RemovesWhatItAdded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_HOME", filepath.Join(home, ".amux"))
	p := AGYSettingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"theme":"dark"}`), 0o600)

	if err := HookAgy(""); err != nil {
		t.Fatal(err)
	}
	if err := UnhookAgy(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(p)
	_ = json.Unmarshal(b, &m)
	if _, ok := m["modelProvider"]; ok || m["theme"] != "dark" {
		t.Fatalf("modelProvider amux added was not removed: %s", b)
	}
}

func TestUnhookCodex_RemovesFilesItCreated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := HookCodex(""); err != nil {
		t.Fatal(err)
	}
	if err := UnhookCodex(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{CodexConfigPath(), CodexTomlPath()} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s left behind after unhook", f)
		}
	}
}

func TestShellRC_UnhookNeverCreatesOrBreaksSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	if err := syncAgyShellRC("", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("unhook created ~/.zshrc")
	}

	real := filepath.Join(home, "dotfiles", "zshrc")
	_ = os.MkdirAll(filepath.Dir(real), 0o755)
	_ = os.WriteFile(real, []byte("export A=1\n"), 0o600)
	_ = os.Symlink(real, filepath.Join(home, ".zshrc"))
	if err := syncAgyShellRC("http://127.0.0.1:8787", true); err != nil {
		t.Fatal(err)
	}
	if err := syncAgyShellRC("", false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(home, ".zshrc")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlinked rc was replaced by a regular file")
	}
	b, _ := os.ReadFile(real)
	st, _ := os.Stat(real)
	if string(b) != "export A=1\n" || st.Mode().Perm() != 0o600 {
		t.Fatalf("rc not restored exactly: %q %v", b, st.Mode().Perm())
	}
}
