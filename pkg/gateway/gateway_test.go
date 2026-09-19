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

func TestGateway_IsHooked_IgnoresExternalURLs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-ext-url-test")
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

	// Pre-create ~/.codex/config.json with corporate / third-party proxy
	codexDir := tmpDir + "/.codex"
	_ = os.MkdirAll(codexDir, 0o755)
	_ = os.WriteFile(codexDir+"/config.json", []byte(`{"openai_base_url":"https://api.openai.com/v1"}`), 0o600)

	// Pre-create ~/.codex/config.toml with third-party proxy
	_ = os.WriteFile(codexDir+"/config.toml", []byte("openai_base_url = \"https://corp.proxy.internal/v1\"\n"), 0o600)

	hooked, val := gateway.IsCodexHooked()
	if hooked {
		t.Fatalf("IsCodexHooked should ignore non-amux URLs, got hooked=%v val=%s", hooked, val)
	}

	// UnhookCodex must NOT delete user's non-amux URLs
	if err := gateway.UnhookCodex(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(codexDir + "/config.json")
	if !strings.Contains(string(b), "https://api.openai.com/v1") {
		t.Fatalf("UnhookCodex deleted non-amux URL in JSON: %s", string(b))
	}
	bt, _ := os.ReadFile(codexDir + "/config.toml")
	if !strings.Contains(string(bt), "https://corp.proxy.internal/v1") {
		t.Fatalf("UnhookCodex deleted non-amux URL in TOML: %s", string(bt))
	}
}

func TestGateway_HookAgy_PreservesUserCredentials(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-agy-user-creds-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	origEnv, _ := exec.Command("launchctl", "getenv", "GOOGLE_GEMINI_BASE_URL").Output()
	_ = exec.Command("launchctl", "unsetenv", "GOOGLE_GEMINI_BASE_URL").Run()
	origKeyEnv, _ := exec.Command("launchctl", "getenv", "GEMINI_API_KEY").Output()
	_ = exec.Command("launchctl", "unsetenv", "GEMINI_API_KEY").Run()
	defer func() {
		if strings.TrimSpace(string(origEnv)) != "" {
			_ = exec.Command("launchctl", "setenv", "GOOGLE_GEMINI_BASE_URL", strings.TrimSpace(string(origEnv))).Run()
		}
		if strings.TrimSpace(string(origKeyEnv)) != "" {
			_ = exec.Command("launchctl", "setenv", "GEMINI_API_KEY", strings.TrimSpace(string(origKeyEnv))).Run()
		}
	}()

	// Pre-create ~/.gemini/antigravity-cli/settings.json with user credentials
	agyDir := tmpDir + "/.gemini/antigravity-cli"
	_ = os.MkdirAll(agyDir, 0o755)
	userSettings := `{
  "modelProvider": "custom-provider",
  "env": {
    "GEMINI_API_KEY": "user-preexisting-secret-key",
    "OTHER_VAR": "keep-me"
  }
}`
	_ = os.WriteFile(agyDir+"/settings.json", []byte(userSettings), 0o600)

	testURL := "http://127.0.0.1:8787"
	if err := gateway.HookAgy(testURL); err != nil {
		t.Fatalf("HookAgy error: %v", err)
	}

	// Verify user's modelProvider and GEMINI_API_KEY are NOT overwritten
	b, err := os.ReadFile(agyDir + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, `"modelProvider": "custom-provider"`) {
		t.Fatalf("expected custom modelProvider preserved, got: %s", content)
	}
	if !strings.Contains(content, `"GEMINI_API_KEY": "user-preexisting-secret-key"`) {
		t.Fatalf("expected user GEMINI_API_KEY preserved, got: %s", content)
	}
	if !strings.Contains(content, `"GOOGLE_GEMINI_BASE_URL": "http://127.0.0.1:8787"`) {
		t.Fatalf("expected GOOGLE_GEMINI_BASE_URL added, got: %s", content)
	}

	// Unhook AGY
	if err := gateway.UnhookAgy(); err != nil {
		t.Fatalf("UnhookAgy error: %v", err)
	}

	// Verify user credentials remain intact after unhook
	bAfter, err := os.ReadFile(agyDir + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	afterContent := string(bAfter)
	if strings.Contains(afterContent, "GOOGLE_GEMINI_BASE_URL") {
		t.Fatalf("expected GOOGLE_GEMINI_BASE_URL removed, got: %s", afterContent)
	}
	if !strings.Contains(afterContent, `"GEMINI_API_KEY": "user-preexisting-secret-key"`) {
		t.Fatalf("expected user GEMINI_API_KEY preserved after unhook, got: %s", afterContent)
	}
	if !strings.Contains(afterContent, `"modelProvider": "custom-provider"`) {
		t.Fatalf("expected custom modelProvider preserved after unhook, got: %s", afterContent)
	}
	if !strings.Contains(afterContent, `"OTHER_VAR": "keep-me"`) {
		t.Fatalf("expected OTHER_VAR preserved after unhook, got: %s", afterContent)
	}
}

func TestGateway_UnhookCodex_PropagatesMalformedJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-codex-malformed-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	codexDir := tmpDir + "/.codex"
	_ = os.MkdirAll(codexDir, 0o755)
	_ = os.WriteFile(codexDir+"/config.json", []byte(`{invalid-json`), 0o600)

	err = gateway.UnhookCodex()
	if err == nil {
		t.Fatalf("expected UnhookCodex to return an error for malformed config.json, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error message, got: %v", err)
	}
}

