package cli

import (
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
)

// CmdDoctor runs diagnostics for network, Keychain access, tool compatibility, and daemon health.
func CmdDoctor(args []string) {
	fmt.Println("== AMUX Diagnostic Doctor ==")

	// 1. macOS Keychain Access
	fmt.Print("[Keychain] Checking OS Keychain read/write access... ")
	if ok, msg := identity.ProbeKeychainAccess(); ok {
		fmt.Printf("OK (%s)\n", msg)
	} else {
		fmt.Printf("FAIL (%s)\n", msg)
	}

	// 2. Gateway Daemon Status
	fmt.Print("[Gateway] Checking gateway daemon health... ")
	if gateway.IsRunning() {
		fmt.Printf("RUNNING (:8787 responding)\n")
	} else {
		fmt.Printf("STOPPED (Normal if operating in zero-touch mode)\n")
	}

	// 3. Network Upstream Connectivity
	fmt.Print("[Network] Checking Anthropic API connectivity... ")
	c := http.Client{Timeout: 3 * time.Second}
	if resp, err := c.Get("https://api.anthropic.com"); err == nil {
		_ = resp.Body.Close()
		fmt.Printf("OK (%d)\n", resp.StatusCode)
	} else {
		fmt.Printf("WARN (%v)\n", err)
	}

	fmt.Print("[Network] Checking OpenAI API connectivity... ")
	if resp, err := c.Get("https://api.openai.com"); err == nil {
		_ = resp.Body.Close()
		fmt.Printf("OK (%d)\n", resp.StatusCode)
	} else {
		fmt.Printf("WARN (%v)\n", err)
	}

	// 4. Client Tools Compatibility
	fmt.Println("\n== Client IDE & Tool Detection ==")
	checkTool("Claude Code", "claude", hook.ClaudeAvailable())
	checkTool("Codex CLI", "codex", hook.CodexAvailable())
	checkTool("Cursor", "cursor", hook.CursorAvailable())
	checkTool("Gemini / AGY", "agy", hook.GeminiAvailable())

	// 5. Identities Health Check
	fmt.Println("\n== Identity Health Check ==")
	reports, err := identity.CheckAllHealth("")
	if err != nil || len(reports) == 0 {
		fmt.Println("No identities configured.")
	} else {
		for _, r := range reports {
			fmt.Printf(" - [%s] %s (%s): %s [%s]\n", r.Provider, r.ID, r.Tier, r.Status, r.Message)
		}
	}

	fmt.Println("\nDiagnostics complete.")
}

func checkTool(name, bin string, available bool) {
	if available {
		path, _ := exec.LookPath(bin)
		fmt.Printf("✓ %-14s: Available (%s)\n", name, path)
	} else {
		fmt.Printf("- %-14s: Not found on PATH\n", name)
	}
}
