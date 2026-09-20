package env

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amux-accounts/pkg/types"
)

// EnvPath returns ~/.amux/env.json (or $AM_HOME/env.json).
func EnvPath() string {
	return filepath.Join(types.BaseDir(), "env.json")
}

// LoadEnvVars loads custom key-value pairs persisted in ~/.amux/env.json.
func LoadEnvVars() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(EnvPath())
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

// SaveEnvVars saves key-value pairs into ~/.amux/env.json.
func SaveEnvVars(m map[string]string) error {
	if err := os.MkdirAll(types.BaseDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(EnvPath(), append(b, '\n'), 0o600)
}

// ShellQuote wraps a string in single quotes safely for POSIX shells.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PrintEnvExports outputs shell export lines for eval "$(amux env)".
//
// When the gateway/proxy is up, it exports the endpoints and placeholder keys
// needed by Claude Code, Codex, and Antigravity CLI (agy).
// When down, it unsets those variables so sessions fall through to native APIs.
func PrintEnvExports(proxyUp bool, hasProfiles bool, proxyBase string) {
	if proxyBase == "" {
		proxyBase = "http://127.0.0.1:8787"
	}
	m := LoadEnvVars()

	if proxyUp {
		fmt.Printf("export ANTHROPIC_BASE_URL=%s\n", proxyBase)
		fmt.Printf("export ANTHROPIC_AUTH_TOKEN=amux-local\n")
		fmt.Printf("export ANTHROPIC_MODEL=claude-sonnet-5\n")
		fmt.Printf("export OPENAI_BASE_URL=%s/v1\n", proxyBase)
		fmt.Printf("export OPENAI_API_KEY=amux-local\n")
		fmt.Printf("export GEMINI_API_BASE=%s\n", proxyBase)
		fmt.Printf("export GOOGLE_GENAI_BASE_URL=%s\n", proxyBase)
		fmt.Printf("export GOOGLE_GEMINI_BASE_URL=%s\n", proxyBase)
		fmt.Printf("export GEMINI_API_KEY=amux-local\n")
		fmt.Printf("export GOOGLE_GENAI_API_KEY=amux-local\n")
	} else {
		if _, ok := m["ANTHROPIC_BASE_URL"]; !ok {
			fmt.Printf("unset ANTHROPIC_BASE_URL\n")
		}
		if _, ok := m["ANTHROPIC_AUTH_TOKEN"]; !ok {
			fmt.Printf("unset ANTHROPIC_AUTH_TOKEN\n")
		}
		if _, ok := m["ANTHROPIC_MODEL"]; !ok {
			fmt.Printf("unset ANTHROPIC_MODEL\n")
		}
		if _, ok := m["OPENAI_BASE_URL"]; !ok {
			fmt.Printf("unset OPENAI_BASE_URL\n")
		}
		if _, ok := m["OPENAI_API_KEY"]; !ok {
			fmt.Printf("unset OPENAI_API_KEY\n")
		}
		if _, ok := m["GEMINI_API_BASE"]; !ok {
			fmt.Printf("unset GEMINI_API_BASE\n")
		}
		if _, ok := m["GOOGLE_GENAI_BASE_URL"]; !ok {
			fmt.Printf("unset GOOGLE_GENAI_BASE_URL\n")
		}
		if _, ok := m["GOOGLE_GEMINI_BASE_URL"]; !ok {
			fmt.Printf("unset GOOGLE_GEMINI_BASE_URL\n")
		}
		if _, ok := m["GEMINI_API_KEY"]; !ok {
			fmt.Printf("unset GEMINI_API_KEY\n")
		}
		if _, ok := m["GOOGLE_GENAI_API_KEY"]; !ok {
			fmt.Printf("unset GOOGLE_GENAI_API_KEY\n")
		}
		if _, ok := m["GOOGLE_API_KEY"]; !ok {
			fmt.Printf("unset GOOGLE_API_KEY\n")
		}
	}

	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if proxyUp && (k == "ANTHROPIC_AUTH_TOKEN" || k == "ANTHROPIC_BASE_URL" || k == "OPENAI_BASE_URL" || k == "OPENAI_API_KEY" || k == "GEMINI_API_BASE" || k == "GOOGLE_GENAI_BASE_URL" || k == "GOOGLE_GEMINI_BASE_URL" || k == "GEMINI_API_KEY" || k == "GOOGLE_GENAI_API_KEY") {
			continue
		}
		fmt.Printf("export %s=%s\n", k, ShellQuote(m[k]))
	}
}
