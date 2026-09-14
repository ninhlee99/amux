package env

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintEnvExports_ProxyUpIncludesGatewayCreds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AM_HOME", dir)
	_ = os.WriteFile(filepath.Join(dir, "env.json"), []byte(`{"FOO":"bar"}`), 0o600)

	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintEnvExports(true, true, "http://127.0.0.1:8787")
	_ = w.Close()
	os.Stdout = old
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if !strings.Contains(out, "export ANTHROPIC_BASE_URL=http://127.0.0.1:8787\n") {
		t.Fatalf("missing BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export ANTHROPIC_AUTH_TOKEN=am-proxy\n") {
		t.Fatalf("missing AUTH_TOKEN: %q", out)
	}
	if !strings.Contains(out, "export OPENAI_BASE_URL=http://127.0.0.1:8787/v1\n") {
		t.Fatalf("missing OPENAI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export OPENAI_API_KEY=am-proxy\n") {
		t.Fatalf("missing OPENAI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "alias codex='am run codex'\n") {
		t.Fatalf("missing codex alias: %q", out)
	}
	if !strings.Contains(out, "export GEMINI_API_BASE=http://127.0.0.1:8787\n") {
		t.Fatalf("missing GEMINI_API_BASE: %q", out)
	}
	if !strings.Contains(out, "export GOOGLE_GENAI_BASE_URL=http://127.0.0.1:8787\n") {
		t.Fatalf("missing GOOGLE_GENAI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787\n") {
		t.Fatalf("missing GOOGLE_GEMINI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export GEMINI_API_KEY=am-proxy\n") {
		t.Fatalf("missing GEMINI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "export GOOGLE_GENAI_API_KEY=am-proxy\n") {
		t.Fatalf("missing GOOGLE_GENAI_API_KEY: %q", out)
	}
	if strings.Contains(out, "GOOGLE_API_KEY") {
		t.Fatalf("must not export GOOGLE_API_KEY (poisons gcloud/Maps): %q", out)
	}
	if !strings.Contains(out, "alias agy='am run agy'\n") {
		t.Fatalf("missing agy alias: %q", out)
	}
	if !strings.Contains(out, "export FOO='bar'\n") {
		t.Fatalf("missing custom env: %q", out)
	}
}

func TestPrintEnvExports_ProxyDownUnsetsAnthropic(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintEnvExports(false, false, "http://127.0.0.1:8787")
	_ = w.Close()
	os.Stdout = old
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	// A shell that ran `eval "$(am env)"` while the proxy was up has these
	// exported live — omitting the line (old behavior) left them stale.
	// Must unset explicitly so the shell falls through to api.anthropic.com or upstream Google.
	if !strings.Contains(out, "unset ANTHROPIC_BASE_URL\n") {
		t.Fatalf("missing unset BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset ANTHROPIC_AUTH_TOKEN\n") {
		t.Fatalf("missing unset AUTH_TOKEN: %q", out)
	}
	if !strings.Contains(out, "unset OPENAI_BASE_URL\n") {
		t.Fatalf("missing unset OPENAI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset OPENAI_API_KEY\n") {
		t.Fatalf("missing unset OPENAI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "unset GEMINI_API_BASE\n") {
		t.Fatalf("missing unset GEMINI_API_BASE: %q", out)
	}
	if !strings.Contains(out, "unset GOOGLE_GENAI_BASE_URL\n") {
		t.Fatalf("missing unset GOOGLE_GENAI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset GOOGLE_GEMINI_BASE_URL\n") {
		t.Fatalf("missing unset GOOGLE_GEMINI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset GEMINI_API_KEY\n") {
		t.Fatalf("missing unset GEMINI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "unset GOOGLE_GENAI_API_KEY\n") {
		t.Fatalf("missing unset GOOGLE_GENAI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "unset GOOGLE_API_KEY\n") {
		t.Fatalf("missing unset GOOGLE_API_KEY (legacy dummy): %q", out)
	}
	if strings.Contains(out, "export ANTHROPIC_") || strings.Contains(out, "export GEMINI_") {
		t.Fatalf("should not export gateway vars when proxy down: %q", out)
	}
}

func TestPrintEnvExports_ProxyDownRespectsUserOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AM_HOME", dir)
	_ = os.WriteFile(filepath.Join(dir, "env.json"), []byte(`{"ANTHROPIC_BASE_URL":"https://my-gateway.example.com"}`), 0o600)

	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintEnvExports(false, true, "http://127.0.0.1:8787")
	_ = w.Close()
	os.Stdout = old
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if strings.Contains(out, "unset ANTHROPIC_BASE_URL\n") {
		t.Fatalf("should not unset a user-configured override: %q", out)
	}
	if !strings.Contains(out, "export ANTHROPIC_BASE_URL='https://my-gateway.example.com'\n") {
		t.Fatalf("missing user override export: %q", out)
	}
	if !strings.Contains(out, "unset ANTHROPIC_AUTH_TOKEN\n") {
		t.Fatalf("missing unset AUTH_TOKEN: %q", out)
	}
}
