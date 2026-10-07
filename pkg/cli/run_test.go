package cli

import (
	"testing"
)

func TestCmdRun_HelpNoArgs(t *testing.T) {
	// Calling with no args or help should print usage and return safely without exit
	CmdRun([]string{})
	CmdRun([]string{"--help"})
	CmdRun([]string{"-h"})
	CmdRun([]string{"help"})
}

func TestPrepareSandboxEnv_AllTargets(t *testing.T) {
	gw := "http://127.0.0.1:8787"

	// 1. Claude
	bin, env := PrepareSandboxEnv("claude", gw)
	if bin != "claude" || env["ANTHROPIC_BASE_URL"] != gw || env["AMUX_SANDBOX"] != "1" || env["ANTHROPIC_API_KEY"] == "" {
		t.Fatalf("claude env mismatch: bin=%s, env=%+v", bin, env)
	}

	// 2. Cursor
	bin, env = PrepareSandboxEnv("cursor", gw)
	if bin != "cursor" || env["OPENAI_BASE_URL"] != gw+"/v1" || env["OPENAI_API_BASE"] != gw+"/v1" || env["OPENAI_API_KEY"] == "" {
		t.Fatalf("cursor env mismatch: bin=%s, env=%+v", bin, env)
	}

	// 3. Windsurf
	bin, env = PrepareSandboxEnv("windsurf", gw)
	if bin != "windsurf" || env["OPENAI_BASE_URL"] != gw+"/v1" || env["OPENAI_API_BASE"] != gw+"/v1" || env["ANTHROPIC_BASE_URL"] != gw || env["ANTHROPIC_API_KEY"] == "" {
		t.Fatalf("windsurf env mismatch: bin=%s, env=%+v", bin, env)
	}

	// 4. Codex
	bin, env = PrepareSandboxEnv("codex", gw)
	if bin != "codex" || env["OPENAI_BASE_URL"] != gw+"/v1" || env["OPENAI_API_BASE"] != gw+"/v1" {
		t.Fatalf("codex env mismatch: bin=%s, env=%+v", bin, env)
	}

	// 5. AGY
	bin, env = PrepareSandboxEnv("agy", gw)
	if bin != "agy" && bin != "antigravity" {
		t.Fatalf("agy bin mismatch: bin=%s", bin)
	}
	if env["GOOGLE_GEMINI_BASE_URL"] != gw || env["GEMINI_API_BASE"] != gw || env["GOOGLE_GENAI_BASE_URL"] != gw {
		t.Fatalf("agy env mismatch: env=%+v", env)
	}

	// 6. VS Code
	bin, env = PrepareSandboxEnv("vscode", gw)
	if bin != "code" || env["OPENAI_BASE_URL"] != gw+"/v1" || env["ANTHROPIC_BASE_URL"] != gw {
		t.Fatalf("vscode env mismatch: bin=%s, env=%+v", bin, env)
	}

	// 7. Arbitrary CLI tool (e.g. aider, opencode)
	bin, env = PrepareSandboxEnv("aider", gw)
	if bin != "aider" {
		t.Fatalf("arbitrary bin mismatch: bin=%s", bin)
	}
	if env["OPENAI_BASE_URL"] != gw+"/v1" || env["OPENAI_API_BASE"] != gw+"/v1" || env["ANTHROPIC_BASE_URL"] != gw || env["GOOGLE_GEMINI_BASE_URL"] != gw || env["GEMINI_API_BASE"] != gw || env["GOOGLE_GENAI_BASE_URL"] != gw {
		t.Fatalf("arbitrary env mismatch: env=%+v", env)
	}
	if env["OPENAI_API_KEY"] != "am-proxy" || env["ANTHROPIC_AUTH_TOKEN"] != "am-proxy" || env["ANTHROPIC_API_KEY"] != "am-proxy" {
		t.Fatalf("arbitrary missing fallback tokens: env=%+v", env)
	}
}

func TestResolveBinaryPath_Standard(t *testing.T) {
	// Standard shell command should resolve
	p, err := ResolveBinaryPath("sh")
	if err != nil || p == "" {
		t.Fatalf("expected 'sh' to resolve, got %v (%s)", err, p)
	}

	// Bogus command should return clean error
	_, err = ResolveBinaryPath("non_existent_binary_xyz_999")
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
}

func TestPrepareSandboxEnv_ProviderPinning(t *testing.T) {
	gw := "http://127.0.0.1:8787/p/chatgpt:01"

	// Claude with provider path
	bin, env := PrepareSandboxEnv("claude", gw)
	if bin != "claude" || env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787/p/chatgpt:01" {
		t.Fatalf("claude provider pinning mismatch: %+v", env)
	}

	// Cursor with provider path
	bin, env = PrepareSandboxEnv("cursor", gw)
	if bin != "cursor" || env["OPENAI_BASE_URL"] != "http://127.0.0.1:8787/p/chatgpt:01/v1" {
		t.Fatalf("cursor provider pinning mismatch: %+v", env)
	}
}

