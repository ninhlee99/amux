package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/provider"
)

// PrepareSandboxEnv computes the binary name and environment variables to inject.
func PrepareSandboxEnv(target, gatewayURL string) (string, map[string]string) {
	envOverrides := map[string]string{
		"AMUX_SANDBOX": "1",
	}

	var binName string
	switch target {
	case "claude", "claude-code":
		binName = "claude"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			envOverrides["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
	case "cursor":
		binName = "cursor"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["OPENAI_API_BASE"] = gatewayURL + "/v1"
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
	case "windsurf", "windsurf-cli":
		binName = "windsurf"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["OPENAI_API_BASE"] = gatewayURL + "/v1"
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			envOverrides["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
	case "codex", "codex-cli":
		binName = "codex"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["OPENAI_API_BASE"] = gatewayURL + "/v1"
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
	case "agy", "antigravity":
		binName = "agy"
		if _, err := exec.LookPath("agy"); err != nil {
			if _, err2 := exec.LookPath("antigravity"); err2 == nil {
				binName = "antigravity"
			}
		}
		envOverrides["GOOGLE_GEMINI_BASE_URL"] = gatewayURL
		envOverrides["GEMINI_API_BASE"] = gatewayURL
		envOverrides["GOOGLE_GENAI_BASE_URL"] = gatewayURL
		if os.Getenv("GEMINI_API_KEY") == "" {
			envOverrides["GEMINI_API_KEY"] = "am-proxy"
		}
	case "aider":
		binName = "aider"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["OPENAI_API_BASE"] = gatewayURL + "/v1"
		envOverrides["GOOGLE_GEMINI_BASE_URL"] = gatewayURL
		envOverrides["GEMINI_API_BASE"] = gatewayURL
		envOverrides["GOOGLE_GENAI_BASE_URL"] = gatewayURL
		envOverrides["OLLAMA_API_BASE"] = gatewayURL
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			envOverrides["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
		if os.Getenv("GEMINI_API_KEY") == "" {
			envOverrides["GEMINI_API_KEY"] = "am-proxy"
		}
	case "opencode":
		binName = "opencode"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
	case "cline", "roo", "roo-code":
		binName = target
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
	case "zed":
		binName = "zed"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
	default:
		// Arbitrary command execution with universal proxy variables
		binName = target
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["OPENAI_API_BASE"] = gatewayURL + "/v1"
		envOverrides["GOOGLE_GEMINI_BASE_URL"] = gatewayURL
		envOverrides["GEMINI_API_BASE"] = gatewayURL
		envOverrides["GOOGLE_GENAI_BASE_URL"] = gatewayURL
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			envOverrides["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			envOverrides["ANTHROPIC_API_KEY"] = "am-proxy"
		}
		if os.Getenv("GEMINI_API_KEY") == "" {
			envOverrides["GEMINI_API_KEY"] = "am-proxy"
		}
	}
	return binName, envOverrides
}

// ResolveBinaryPath locates the target executable in PATH, standard bin directories, or standard application folders.
func ResolveBinaryPath(binName string) (string, error) {
	if p, err := exec.LookPath(binName); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	// Fallback check standard binary paths in case shell PATH is stripped
	commonBinDirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".pyenv", "shims"),
		filepath.Join(home, ".local", "pipx", "venvs", binName, "bin"),
		filepath.Join(home, "Library", "Python", "3.12", "bin"),
		filepath.Join(home, "Library", "Python", "3.11", "bin"),
		filepath.Join(home, "Library", "Python", "3.10", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".npm-global", "bin"),
		"/usr/bin",
		"/bin",
	}
	for _, dir := range commonBinDirs {
		candidate := filepath.Join(dir, binName)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() && (fi.Mode()&0111 != 0) {
			return candidate, nil
		}
	}
	// Fallback check on macOS application bundles
	var candidates []string
	switch strings.ToLower(binName) {
	case "cursor":
		candidates = []string{
			"/Applications/Cursor.app/Contents/MacOS/Cursor",
			filepath.Join(home, "Applications/Cursor.app/Contents/MacOS/Cursor"),
		}
	case "windsurf":
		candidates = []string{
			"/Applications/Windsurf.app/Contents/MacOS/Windsurf",
			filepath.Join(home, "Applications/Windsurf.app/Contents/MacOS/Windsurf"),
		}
	case "agy", "antigravity":
		candidates = []string{
			"/Applications/Antigravity.app/Contents/MacOS/Antigravity",
			filepath.Join(home, "Applications/Antigravity.app/Contents/MacOS/Antigravity"),
		}
	case "zed":
		candidates = []string{
			"/Applications/Zed.app/Contents/MacOS/cli",
			"/Applications/Zed.app/Contents/MacOS/Zed",
			filepath.Join(home, "Applications/Zed.app/Contents/MacOS/Zed"),
		}
	case "code", "vscode":
		candidates = []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			"/usr/local/bin/code",
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("executable '%s' not found in PATH or standard applications", binName)
}

// CmdRun executes a coding IDE agent (Claude Code, Cursor, Windsurf, AGY, Codex)
// in an isolated sandbox environment with gateway variables injected into
// the child process only, without mutating global system configurations or launchctl.
func CmdRun(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: amux run <ide> [args...]")
		fmt.Println("Supported IDEs: claude, cursor, windsurf, codex, agy (antigravity)")
		return
	}

	target := strings.ToLower(args[0])
	extraArgs := args[1:]

	gatewayURL := "http://127.0.0.1:8787"
	if customPort := os.Getenv("AMUX_PORT"); customPort != "" {
		gatewayURL = "http://127.0.0.1:" + customPort
	}

	binName, envOverrides := PrepareSandboxEnv(target, gatewayURL)

	binPath, err := ResolveBinaryPath(binName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux run: %v\n", err)
		os.Exit(1)
	}

	// Auto-check if gateway is up and wait until ready
	gw := gateway.GetStatus()
	if !gw.Running {
		fmt.Printf("➜ AMUX Gateway (:8787) is not running. Starting background gateway...\n")
		cmdGatewayStart(nil)
		// Wait up to 3s for gateway to be fully responsive
		for i := 0; i < 30; i++ {
			if gateway.IsRunning() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !gateway.IsRunning() {
			fmt.Fprintf(os.Stderr, "⚠ Warning: Gateway (:8787) is taking longer than usual to start. Run 'amux doctor' if requests fail.\n")
		}
	}

	// Check if accounts exist to give immediate helpful guidance
	hasAccounts := false
	if cfg, err := identity.LoadConfig(""); err == nil && len(cfg.Identities) > 0 {
		hasAccounts = true
	} else if accts, err := provider.LoadAccounts(provider.DefaultAccountsPath()); err == nil && len(accts) > 0 {
		hasAccounts = true
	}
	if !hasAccounts {
		fmt.Println("⚠ Notice: No accounts configured in AMUX yet. Run 'amux login' to connect Claude, ChatGPT, Gemini, or an API key.")
	}

	fmt.Printf("⚡ AMUX Sandbox: %s → %s (isolated session)\n", binName, gatewayURL)
	if target == "cursor" {
		cm := hook.LoadCursorSettings()
		base, ok := cm["cursor.general.openaiBaseUrl"].(string)
		base2, ok2 := cm["openai.baseUrl"].(string)
		if (ok && strings.TrimSpace(base) != "") || (ok2 && strings.TrimSpace(base2) != "") {
			fmt.Printf("✓ Cursor AI Chat already configured to use AMUX Gateway (%s/v1)\n", gatewayURL)
		} else {
			fmt.Printf("💡 Tip: For Cursor GUI AI Chat, ensure Cursor Settings > Models > 'Override OpenAI Base URL' is set to %s/v1 (or run 'amux hook cursor' to auto-configure).\n", gatewayURL)
		}
	} else if target == "windsurf" {
		fmt.Printf("💡 Tip: For Windsurf Cascade GUI Chat, configure OpenAI Base URL to %s/v1 in Settings > Models\n", gatewayURL)
	} else if target == "aider" {
		fmt.Printf("💡 Tip: Aider is pre-configured with AMUX. Run with '--model openai/gpt-4o' or '--model anthropic/claude-3-5-sonnet'\n")
	} else if target == "opencode" {
		fmt.Printf("💡 Tip: Opencode is pre-configured with proxy baseUrl %s/v1\n", gatewayURL)
	}

	// Build process environment
	cmdEnv := os.Environ()
	for k, v := range envOverrides {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.Command(binPath, extraArgs...)
	cmd.Env = cmdEnv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Forward termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "amux run error: %v\n", err)
		os.Exit(1)
	}
}

func helpRun() {
	fmt.Println("Usage: amux run <ide> [args...]")
	fmt.Println("  Runs the selected IDE or coding agent in a sandboxed session")
	fmt.Println("  pointing to the AMUX Gateway without altering global system files.")
	fmt.Println()
	fmt.Println("Supported IDE & agent targets:")
	fmt.Println("  claude    - Runs Claude Code CLI with ANTHROPIC_BASE_URL injected")
	fmt.Println("  cursor    - Runs Cursor IDE with OPENAI_BASE_URL injected")
	fmt.Println("  windsurf  - Runs Windsurf IDE with OPENAI_BASE_URL & ANTHROPIC_BASE_URL injected")
	fmt.Println("  codex     - Runs OpenAI Codex CLI with OPENAI_BASE_URL injected")
	fmt.Println("  agy       - Runs Google Antigravity CLI with GEMINI_BASE_URL injected")
	fmt.Println("  aider     - Runs Aider CLI with OPENAI_API_BASE & ANTHROPIC_BASE_URL injected")
	fmt.Println("  opencode  - Runs OpenCode agent with OPENAI_BASE_URL injected")
	fmt.Println("  zed       - Runs Zed editor with AI proxy variables injected")
	fmt.Println("  <cmd>     - Arbitrary agent or tool with all AI proxy variables preconfigured")
}
