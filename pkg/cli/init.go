package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/types"
)

// CmdInit runs the interactive / zero-friction first-time developer onboarding wizard.
func CmdInit(args []string) {
	fmt.Println("================================================================")
	fmt.Println("       Welcome to AMUX — AI-Native Development Environment      ")
	fmt.Println("================================================================")
	fmt.Println("AMUX serves as your collaborative intelligence layer, managing")
	fmt.Println("AI identities, terminal sessions, and proxy failover non-invasively.")
	fmt.Println()

	baseDir := types.BaseDir()
	fmt.Printf("[1/4] Initializing AMUX workspace directory (%s)... ", baseDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		fmt.Printf("FAIL (%v)\n", err)
	} else {
		_ = os.Chmod(baseDir, 0o700)
		fmt.Println("OK (0700 private)")
	}

	// Initialize sessions store directory
	sessionsDir := filepath.Join(baseDir, "sessions")
	_ = os.MkdirAll(sessionsDir, 0o700)

	// Initialize logs directory
	logsDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(logsDir, 0o700)

	// 2. Keychain & Vault Security Check
	fmt.Print("[2/4] Verifying native Credential Storage (Keychain / Vault)... ")
	if ok, msg := identity.ProbeKeychainAccess(); ok {
		fmt.Printf("OK (%s)\n", msg)
	} else {
		fmt.Printf("NOTICE (%s)\n", msg)
	}

	// 3. AI IDE & CLI Tool Discovery
	fmt.Println("[3/4] Scanning installed AI developer tools:")
	detected := 0
	if hook.ClaudeAvailable() {
		fmt.Println("  ✓ Claude Code CLI detected")
		detected++
	}
	if hook.CodexAvailable() {
		fmt.Println("  ✓ Codex CLI detected")
		detected++
	}
	if hook.CursorAvailable() {
		fmt.Println("  ✓ Cursor IDE detected")
		detected++
	}
	if hook.GeminiAvailable() {
		fmt.Println("  ✓ Gemini / AGY CLI detected")
		detected++
	}
	if detected == 0 {
		fmt.Println("  • No AI CLI tools detected yet. You can install Claude Code, Codex, or Cursor anytime.")
	}

	// 4. Install slash commands & hooks
	fmt.Print("[4/4] Installing IDE slash command helpers (/amux:feedback)... ")
	if err := hook.InstallSlashCommand("feedback.md", []byte(hook.FeedbackSlashCommandContent)); err != nil {
		fmt.Printf("SKIPPED (%v)\n", err)
	} else {
		fmt.Println("OK")
	}

	// Check UDS socket
	sockPath := monitor.DefaultSocketPath()
	client := monitor.NewUDSClient(sockPath)
	daemonRunning := client.IsDaemonAvailable()

	fmt.Println("----------------------------------------------------------------")
	fmt.Println("✓ AMUX is ready! Here are recommended first steps:")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println("  1. Add an identity:        amux id add claude (or 'amux login')")
	fmt.Println("  2. View active accounts:    amux id list")
	fmt.Println("  3. Start proxy daemon:      amux start")
	fmt.Println("  4. View live telemetry:     amux top")
	fmt.Println("  5. Run system diagnostics:  amux doctor")
	if !daemonRunning {
		fmt.Println("Tip: Run 'amux start' in the background to enable live proxy and socket IPC.")
	}
	fmt.Println("================================================================")
}
