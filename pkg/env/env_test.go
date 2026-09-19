package env

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestPrintEnvExports_ProxyUpIncludesGatewayCreds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMUX_HOME", dir)
	_ = os.WriteFile(filepath.Join(dir, "env.json"), []byte(`{"FOO":"bar"}`), 0o600)

	out := captureOutput(func() {
		PrintEnvExports(true, true, "http://127.0.0.1:8787")
	})

	if !strings.Contains(out, "export ANTHROPIC_BASE_URL=http://127.0.0.1:8787\n") {
		t.Fatalf("missing ANTHROPIC_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export ANTHROPIC_AUTH_TOKEN=amux-local\n") {
		t.Fatalf("missing ANTHROPIC_AUTH_TOKEN: %q", out)
	}
	if !strings.Contains(out, "export OPENAI_BASE_URL=http://127.0.0.1:8787/v1\n") {
		t.Fatalf("missing OPENAI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:8787\n") {
		t.Fatalf("missing GOOGLE_GEMINI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "export GEMINI_API_KEY=amux-local\n") {
		t.Fatalf("missing GEMINI_API_KEY: %q", out)
	}
	if !strings.Contains(out, "export FOO='bar'\n") {
		t.Fatalf("missing custom env FOO: %q", out)
	}
}

func TestPrintEnvExports_ProxyDownUnsetsCreds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMUX_HOME", dir)

	out := captureOutput(func() {
		PrintEnvExports(false, false, "http://127.0.0.1:8787")
	})

	if !strings.Contains(out, "unset ANTHROPIC_BASE_URL\n") {
		t.Fatalf("missing unset ANTHROPIC_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset GOOGLE_GEMINI_BASE_URL\n") {
		t.Fatalf("missing unset GOOGLE_GEMINI_BASE_URL: %q", out)
	}
	if !strings.Contains(out, "unset GEMINI_API_KEY\n") {
		t.Fatalf("missing unset GEMINI_API_KEY: %q", out)
	}
}
