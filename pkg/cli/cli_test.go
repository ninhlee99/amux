package cli

import (
	"io"
	"os"
	"strings"
	"testing"

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
		"Core Commands:",
		"Identity Management:",
		"Gateway:",
		"Configuration & Migration:",
		"System:",
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
