package cli

import (
	"strings"
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
	if bin != "claude" || env["ANTHROPIC_BASE_URL"] != gw || env["AMUX_SANDBOX"] != "1" || env["ANTHROPIC_AUTH_TOKEN"] == "" {
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
	if env["OPENAI_API_KEY"] == "" || env["ANTHROPIC_API_KEY"] == "" {
		t.Fatalf("arbitrary missing fallback tokens: env=%+v", env)
	}
}

func TestPrepareSandboxEnv_SingleAnthropicCredential(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	for _, target := range []string{"claude", "windsurf", "aider", "opencode", "cline", "zed", "vscode", "some-agent"} {
		_, env := PrepareSandboxEnv(target, "http://127.0.0.1:8787")
		_, hasTok := env["ANTHROPIC_AUTH_TOKEN"]
		_, hasKey := env["ANTHROPIC_API_KEY"]
		if hasTok == hasKey {
			t.Fatalf("%s: want exactly one Anthropic credential, got %+v", target, env)
		}
	}
	_, env := PrepareSandboxEnv("claude", "http://127.0.0.1:8787")
	if env["ANTHROPIC_AUTH_TOKEN"] == "" {
		t.Fatalf("claude must use ANTHROPIC_AUTH_TOKEN: %+v", env)
	}
}

func TestPrepareSandboxEnv_ClaudeReusesShellAPIKeyAsToken(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-user")
	_, env := PrepareSandboxEnv("claude", "http://127.0.0.1:8787")
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-user" {
		t.Fatalf("shell credential should be kept: %+v", env)
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("API key must not be injected alongside the token: %+v", env)
	}
}

func TestBuildSandboxEnv_DropsConflictingCredential(t *testing.T) {
	base := []string{"PATH=/bin", "ANTHROPIC_API_KEY=sk-shell", "ANTHROPIC_BASE_URL=https://api.anthropic.com"}
	got := BuildSandboxEnv(base, map[string]string{
		"ANTHROPIC_AUTH_TOKEN": "am-proxy",
		"ANTHROPIC_BASE_URL":   "http://127.0.0.1:8787",
	})
	want := []string{"PATH=/bin", "ANTHROPIC_AUTH_TOKEN=am-proxy", "ANTHROPIC_BASE_URL=http://127.0.0.1:8787"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
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
