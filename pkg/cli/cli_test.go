package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/proxy"
)

func TestToolAndName(t *testing.T) {
	tests := []struct {
		input    []string
		wantTool string
		wantName string
	}{
		{[]string{}, "claude", ""},
		{[]string{"work"}, "claude", "work"},
		{[]string{"user@gmail.com"}, "claude", "user@gmail.com"},
		{[]string{"codex", "personal"}, "codex", "personal"},
		{[]string{"gemini", "my-key"}, "gemini", "my-key"},
		{[]string{"claude1"}, "claude", "claude1"},
		{[]string{"codexcli:01"}, "codex", "codexcli:01"},
		{[]string{"geminicli:02"}, "antigravity", "geminicli:02"},
	}

	for _, tt := range tests {
		tool, name := toolAndName(tt.input)
		if tool != tt.wantTool || name != tt.wantName {
			t.Errorf("toolAndName(%v) = (%q, %q), want (%q, %q)",
				tt.input, tool, name, tt.wantTool, tt.wantName)
		}
	}
}

func TestOrDash(t *testing.T) {
	if orDash("") != "-" {
		t.Errorf("orDash(\"\") = %q, want \"-\"", orDash(""))
	}
	if orDash("hello") != "hello" {
		t.Errorf("orDash(\"hello\") = %q, want \"hello\"", orDash("hello"))
	}
}

func TestProfileName(t *testing.T) {
	if profileName("my work", "test@domain.com") != "my-work" {
		t.Errorf("profileName with custom name failed")
	}
	if profileName("", "test@domain.com") != "test@domain.com" {
		t.Errorf("profileName with empty name should fallback to account")
	}
}

func captureStdout(f func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = orig
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

func TestUsageHelp_ClearCategoriesAndNoInternalNoise(t *testing.T) {
	out := captureStdout(func() {
		usageHelp()
	})

	categories := []string{
		"Gateway Daemon:",
		"Account Management:",
		"Diagnostics & Setup:",
		"Examples:",
	}
	for _, cat := range categories {
		if !strings.Contains(out, cat) {
			t.Errorf("expected usageHelp to contain category %q", cat)
		}
	}

	// Verify launcher is removed
	if strings.Contains(out, "amux run") {
		t.Errorf("expected 'amux run' launcher to be removed from usageHelp")
	}

	// Verify internal noise is stripped from main help
	forbidden := []string{
		"statusline",
		"agy-start",
		"agy-stop",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("expected usageHelp NOT to expose internal/clutter command %q", f)
		}
	}
}

func TestProxyDown_PublicFlag(t *testing.T) {
	// Set public bind
	_ = proxy.SaveBindPublic(true)
	_, _ = proxy.IssueNewAuthToken()
	if !proxy.IsPublic() {
		t.Fatalf("expected IsPublic() true before test")
	}

	// Run proxy down --public via gateway stop
	proxy.CmdProxyDownPublic(true, true)

	if proxy.IsPublic() {
		t.Errorf("expected IsPublic() false after amux proxy down --public")
	}
	tok, _ := proxy.LoadAuthToken()
	if tok != "" {
		t.Errorf("expected empty token after proxy down --public, got: %s", tok)
	}
}

func TestCmdIDAutoRotate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cli-auto-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	// Create test identity
	id := "test-sub-vip"
	_ = CmdID // ensure imported
	cfgPath := filepath.Join(tmpDir, ".am", "identities.json")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)

	// Add via identity package
	upsertErr := CmdID
	_ = upsertErr
	// Run CmdID with auto
	// First upsert an identity directly
	cfg := &identity.Config{
		ThresholdPct: 95.0,
		Identities: []identity.Identity{
			{
				ID:           id,
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 50.0,
				Active:       true,
			},
		},
	}
	if err := identity.SaveConfig("", cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Toggle to off
	CmdID([]string{"auto", id, "off"})
	target, err := identity.Get("", id)
	if err != nil || target == nil {
		t.Fatalf("Get identity error: %v", err)
	}
	if target.CanAutoRotate() {
		t.Errorf("expected CanAutoRotate to be false after 'amux id auto %s off'", id)
	}

	// Toggle back to on
	CmdID([]string{"auto", id, "on"})
	targetOn, err := identity.Get("", id)
	if err != nil || targetOn == nil {
		t.Fatalf("Get identity error: %v", err)
	}
	if !targetOn.CanAutoRotate() {
		t.Errorf("expected CanAutoRotate to be true after 'amux id auto %s on'", id)
	}
}

func TestCmdIDList_ActiveAndAutoSwitchFormatting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cli-list-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	fFalse := false
	cfg := &identity.Config{
		ThresholdPct: 95.0,
		Identities: []identity.Identity{
			{
				ID:           "claude-1",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 30.0,
				Active:       true,
			},
			{
				ID:           "claude-vip",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 10.0,
				Active:       false,
				AutoRotate:   &fFalse,
			},
		},
	}
	if err := identity.SaveConfig("", cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(func() {
		CmdID([]string{"list"})
	})

	if !strings.Contains(out, "ACTIVE") || !strings.Contains(out, "AUTO-SWITCH") {
		t.Errorf("expected header with ACTIVE and AUTO-SWITCH, got:\n%s", out)
	}
	// claude-1 is Active=true, AutoRotate=default(true)
	if !strings.Contains(out, "claude-1") || !strings.Contains(out, "YES") || !strings.Contains(out, "ON") {
		t.Errorf("expected claude-1 to show YES for ACTIVE and ON for AUTO-SWITCH, got:\n%s", out)
	}
	// claude-vip is Active=false, AutoRotate=false
	if !strings.Contains(out, "claude-vip") || !strings.Contains(out, "NO") || !strings.Contains(out, "OFF") {
		t.Errorf("expected claude-vip to show NO for ACTIVE and OFF for AUTO-SWITCH, got:\n%s", out)
	}
	if !strings.Contains(out, "THRESHOLD") {
		t.Errorf("expected THRESHOLD in header, got:\n%s", out)
	}
	if strings.Contains(out, "TIER") {
		t.Errorf("expected TIER to be removed, got:\n%s", out)
	}
}

func TestCmdStatus_ThresholdColumn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cli-status-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	customThresh := 80.0
	cfg := &identity.Config{
		ThresholdPct: 90.0,
		Identities: []identity.Identity{
			{
				ID:           "claude-1",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 30.0,
				Active:       true,
				ThresholdPct: &customThresh,
			},
			{
				ID:           "claude-2",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 10.0,
				Active:       false,
			},
		},
	}
	if err := identity.SaveConfig("", cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	out := captureStdout(func() {
		CmdStatus(nil)
	})

	if !strings.Contains(out, "THRESHOLD") {
		t.Errorf("expected THRESHOLD in CmdStatus output, got:\n%s", out)
	}
	if !strings.Contains(out, "AUTO-SWITCH") {
		t.Errorf("expected AUTO-SWITCH in CmdStatus output, got:\n%s", out)
	}
	if strings.Contains(out, "TIER") {
		t.Errorf("expected TIER to be removed from CmdStatus output, got:\n%s", out)
	}
	// Check table header row does not have PROVIDER column
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if strings.Contains(l, "ID") && strings.Contains(l, "EMAIL") {
			if strings.Contains(l, "PROVIDER") {
				t.Errorf("expected PROVIDER to be removed from header line, got: %s", l)
			}
			if !strings.Contains(l, "AUTO-SWITCH") {
				t.Errorf("expected AUTO-SWITCH in header line, got: %s", l)
			}
		}
	}
	if !strings.Contains(out, "80.0%") {
		t.Errorf("expected custom threshold 80.0%% in CmdStatus output, got:\n%s", out)
	}
	if !strings.Contains(out, "90.0%") {
		t.Errorf("expected default threshold 90.0%% in CmdStatus output, got:\n%s", out)
	}
}

func TestCmdIDThreshold_PerAccount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cli-id-thresh-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	cfg := &identity.Config{
		ThresholdPct: 95.0,
		Identities: []identity.Identity{
			{
				ID:           "claude-acc",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				UsagePercent: 20.0,
				Active:       true,
			},
		},
	}
	if err := identity.SaveConfig("", cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// 1. Set per-account threshold
	CmdID([]string{"threshold", "claude-acc", "85"})
	target, err := identity.Get("", "claude-acc")
	if err != nil || target == nil {
		t.Fatalf("Get identity error: %v", err)
	}
	if target.ThresholdPct == nil || *target.ThresholdPct != 85.0 {
		t.Fatalf("expected ThresholdPct to be 85.0, got %v", target.ThresholdPct)
	}

	// 2. View per-account threshold
	out := captureStdout(func() {
		CmdID([]string{"threshold", "claude-acc"})
	})
	if !strings.Contains(out, "85.0%") || !strings.Contains(out, "custom override") {
		t.Errorf("expected output to show 85.0%% (custom override), got:\n%s", out)
	}

	// 3. Reset per-account threshold
	CmdID([]string{"threshold", "claude-acc", "reset"})
	targetReset, _ := identity.Get("", "claude-acc")
	if targetReset.ThresholdPct != nil {
		t.Fatalf("expected ThresholdPct to be nil after reset, got %v", targetReset.ThresholdPct)
	}

	// 4. Test amux config threshold <id> <val>
	CmdConfig([]string{"threshold", "claude-acc", "75%"})
	targetConfig, _ := identity.Get("", "claude-acc")
	if targetConfig.ThresholdPct == nil || *targetConfig.ThresholdPct != 75.0 {
		t.Fatalf("expected ThresholdPct to be 75.0 via config threshold, got %v", targetConfig.ThresholdPct)
	}
}

func TestCmdAccount_AliasesAndSubcommands(t *testing.T) {
	// Verify CmdAccount help
	out := captureStdout(func() {
		CmdAccount([]string{"help"})
	})
	if !strings.Contains(out, "Usage: amux account <subcommand>") {
		t.Errorf("expected CmdAccount help text, got:\n%s", out)
	}
	if !strings.Contains(out, "switch, select <id>") {
		t.Errorf("expected switch command in help, got:\n%s", out)
	}

	// Verify CmdID calls CmdAccount without panic
	outID := captureStdout(func() {
		CmdID([]string{"help"})
	})
	if !strings.Contains(outID, "Usage: amux account <subcommand>") {
		t.Errorf("expected CmdID to delegate to CmdAccount")
	}
}



