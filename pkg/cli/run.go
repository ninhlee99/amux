package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"amux-accounts/pkg/gateway"
)

// CmdRun executes a coding IDE agent (Claude Code, Cursor, AGY, Codex)
// in an isolated sandbox environment with gateway variables injected into
// the child process only, without mutating global system configurations or launchctl.
func CmdRun(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: amux run <ide> [args...]")
		fmt.Println("Supported IDEs: claude, cursor, codex, agy (antigravity)")
		return
	}

	target := strings.ToLower(args[0])
	extraArgs := args[1:]

	// Determine binary name and environment overrides
	var binName string
	envOverrides := map[string]string{
		"AMUX_SANDBOX": "1",
	}

	gatewayURL := "http://127.0.0.1:8787"
	if customPort := os.Getenv("AMUX_PORT"); customPort != "" {
		gatewayURL = "http://127.0.0.1:" + customPort
	}

	switch target {
	case "claude", "claude-code":
		binName = "claude"
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			envOverrides["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		}
	case "cursor":
		binName = "cursor"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		if os.Getenv("OPENAI_API_KEY") == "" {
			envOverrides["OPENAI_API_KEY"] = "am-proxy"
		}
	case "codex", "codex-cli":
		binName = "codex"
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
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
		if os.Getenv("GEMINI_API_KEY") == "" {
			envOverrides["GEMINI_API_KEY"] = "am-proxy"
		}
	default:
		// Arbitrary command execution with universal proxy variables
		binName = target
		envOverrides["ANTHROPIC_BASE_URL"] = gatewayURL
		envOverrides["OPENAI_BASE_URL"] = gatewayURL + "/v1"
		envOverrides["GOOGLE_GEMINI_BASE_URL"] = gatewayURL
	}

	binPath, err := exec.LookPath(binName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux run: executable '%s' not found in PATH\n", binName)
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

	fmt.Printf("⚡ AMUX Sandbox: %s → %s (isolated session)\n", binName, gatewayURL)

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
	fmt.Println("  Runs the selected IDE (claude, cursor, codex, agy) in a sandboxed session")
	fmt.Println("  pointing to the AMUX Gateway without altering global system files.")
	fmt.Println()
	fmt.Println("Supported IDE targets:")
	fmt.Println("  claude   - Runs Claude Code CLI with ANTHROPIC_BASE_URL injected")
	fmt.Println("  cursor   - Runs Cursor IDE with OPENAI_BASE_URL injected")
	fmt.Println("  codex    - Runs OpenAI Codex CLI with OPENAI_BASE_URL injected")
	fmt.Println("  agy      - Runs Google Antigravity CLI with GEMINI_BASE_URL injected")
}
