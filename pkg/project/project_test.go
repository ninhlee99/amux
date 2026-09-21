package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectConfig_LoadAndSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-proj-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	cfg := &Config{
		Account:  "work-claude-pro",
		Provider: "claude",
	}

	if err := SaveProjectConfig(tmpDir, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Test loading from nested subDir
	loaded, p, err := LoadProjectConfig(subDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected loaded config, got nil")
	}
	if loaded.Account != "work-claude-pro" {
		t.Errorf("expected account 'work-claude-pro', got %q", loaded.Account)
	}
	if filepath.Base(p) != ".amux" {
		t.Errorf("expected path '.amux', got %q", p)
	}
}

func TestProjectConfig_PlainTextFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-proj-plain-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write simple account ID directly to .amux
	plainPath := filepath.Join(tmpDir, ".amux")
	if err := os.WriteFile(plainPath, []byte("dev-gemini-key\n"), 0644); err != nil {
		t.Fatalf("write plain: %v", err)
	}

	loaded, _, err := LoadProjectConfig(tmpDir)
	if err != nil {
		t.Fatalf("load plain: %v", err)
	}
	if loaded == nil || loaded.Account != "dev-gemini-key" {
		t.Fatalf("expected account 'dev-gemini-key', got %+v", loaded)
	}
}
