package gateway_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/identity"
)

func TestGateway_HookClaudeLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-claude-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	// Initially not hooked
	hooked, _ := gateway.IsClaudeHooked()
	if hooked {
		t.Fatalf("expected Claude not hooked initially")
	}

	// Hook Claude
	testURL := "http://127.0.0.1:8787"
	if err := gateway.HookClaude(testURL); err != nil {
		t.Fatalf("HookClaude error: %v", err)
	}

	hooked, val := gateway.IsClaudeHooked()
	if !hooked || val != testURL {
		t.Fatalf("expected hooked with %s, got hooked=%v, val=%s", testURL, hooked, val)
	}

	// Unhook Claude
	if err := gateway.UnhookClaude(); err != nil {
		t.Fatalf("UnhookClaude error: %v", err)
	}

	hooked, _ = gateway.IsClaudeHooked()
	if hooked {
		t.Fatalf("expected Claude unhooked")
	}
}

func TestGateway_ConditionalHookAndAutoDetach(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cond-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	// 1. When accounts are below threshold -> Should NOT hook
	ids := []identity.Identity{
		{
			ID:           "sub-1",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 80.0,
			Active:       true,
		},
	}

	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ := gateway.IsClaudeHooked()
	if hooked {
		t.Errorf("should not hook when subscription has remaining quota")
	}

	// 2. When subscription hits 100% (single account pool rule) -> Injects hook!
	ids[0].UsagePercent = 100.0
	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ = gateway.IsClaudeHooked()
	if !hooked {
		t.Errorf("should hook when all subscriptions are exhausted")
	}

	// 3. When quota resets to 10% -> Auto-detach!
	ids[0].UsagePercent = 10.0
	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ = gateway.IsClaudeHooked()
	if hooked {
		t.Errorf("should auto-detach when subscription resets")
	}
}

func TestGateway_HookCodexLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-codex-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	origEnv, _ := exec.Command("launchctl", "getenv", "OPENAI_BASE_URL").Output()
	_ = exec.Command("launchctl", "unsetenv", "OPENAI_BASE_URL").Run()
	defer func() {
		if strings.TrimSpace(string(origEnv)) != "" {
			_ = exec.Command("launchctl", "setenv", "OPENAI_BASE_URL", strings.TrimSpace(string(origEnv))).Run()
		}
	}()

	hooked, _ := gateway.IsCodexHooked()
	if hooked {
		t.Fatalf("expected Codex not hooked initially")
	}

	testURL := "http://127.0.0.1:8787/v1"
	if err := gateway.HookCodex(testURL); err != nil {
		t.Fatalf("HookCodex error: %v", err)
	}

	hooked, val := gateway.IsCodexHooked()
	if !hooked || val != testURL {
		t.Fatalf("expected hooked with %s, got hooked=%v, val=%s", testURL, hooked, val)
	}

	if err := gateway.UnhookCodex(); err != nil {
		t.Fatalf("UnhookCodex error: %v", err)
	}

	hooked, _ = gateway.IsCodexHooked()
	if hooked {
		t.Fatalf("expected Codex unhooked")
	}
}

func TestGateway_HookCodex_ErrorAndFormatting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-codex-format-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	// Pre-create ~/.codex/config.toml with existing sections
	codexDir := tmpDir + "/.codex"
	_ = os.MkdirAll(codexDir, 0o755)
	initialTOML := "[model]\nname = \"gpt-4o\"\ntemperature = 0.7\n"
	_ = os.WriteFile(codexDir+"/config.toml", []byte(initialTOML), 0o600)

	testURL := "http://127.0.0.1:8787/v1"
	if err := gateway.HookCodex(testURL); err != nil {
		t.Fatalf("HookCodex error: %v", err)
	}

	// Verify TOML preserves previous sections and appends baseURL
	b, err := os.ReadFile(codexDir + "/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	tomlContent := string(b)
	if !strings.Contains(tomlContent, "[model]") || !strings.Contains(tomlContent, "name = \"gpt-4o\"") {
		t.Fatalf("expected TOML to preserve existing sections, got: %s", tomlContent)
	}
	if !strings.Contains(tomlContent, `openai_base_url = "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("expected TOML to contain openai_base_url, got: %s", tomlContent)
	}
	// Semantic root-table assertion: openai_base_url MUST precede [model] so it is not scoped to a child table
	modelIdx := strings.Index(tomlContent, "[model]")
	baseIdx := strings.Index(tomlContent, `openai_base_url = "http://127.0.0.1:8787/v1"`)
	if modelIdx < 0 || baseIdx < 0 || baseIdx >= modelIdx {
		t.Fatalf("openai_base_url must reside at root table scope before [model], got baseIdx=%d, modelIdx=%d", baseIdx, modelIdx)
	}

	// Verify JSON config
	bj, err := os.ReadFile(codexDir + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bj), `"openai_base_url": "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("expected JSON to contain openai_base_url, got: %s", string(bj))
	}
}

func TestGateway_HookAgyLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-agy-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	origEnv, _ := exec.Command("launchctl", "getenv", "GOOGLE_GEMINI_BASE_URL").Output()
	_ = exec.Command("launchctl", "unsetenv", "GOOGLE_GEMINI_BASE_URL").Run()
	defer func() {
		if strings.TrimSpace(string(origEnv)) != "" {
			_ = exec.Command("launchctl", "setenv", "GOOGLE_GEMINI_BASE_URL", strings.TrimSpace(string(origEnv))).Run()
		}
	}()

	hooked, _ := gateway.IsAgyHooked()
	if hooked {
		t.Fatalf("expected AGY not hooked initially")
	}

	testURL := "http://127.0.0.1:8787"
	if err := gateway.HookAgy(testURL); err != nil {
		t.Fatalf("HookAgy error: %v", err)
	}

	hooked, val := gateway.IsAgyHooked()
	if !hooked || val != testURL {
		t.Fatalf("expected hooked with %s, got hooked=%v, val=%s", testURL, hooked, val)
	}

	if err := gateway.UnhookAgy(); err != nil {
		t.Fatalf("UnhookAgy error: %v", err)
	}

	hooked, _ = gateway.IsAgyHooked()
	if hooked {
		t.Fatalf("expected AGY unhooked")
	}
}
