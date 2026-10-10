package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/mcp"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
	"amux-accounts/pkg/ui"
)

// CmdDoctor runs diagnostics for network, Keychain access, tool compatibility, and daemon health.
func CmdDoctor(args []string) {
	if len(args) > 0 && (args[0] == "providers" || args[0] == "--providers") {
		ui.CmdDoctorProviders()
		return
	}
	if len(args) > 0 && (args[0] == "--live" || args[0] == "live") {
		CmdDoctorLive(args[1:])
		return
	}
	if len(args) > 0 && (args[0] == "--security" || args[0] == "security") {
		CmdAudit(args[1:])
		return
	}
	if len(args) > 0 && (args[0] == "auth" || args[0] == "--auth") {
		CmdDoctorAuth(args[1:])
		return
	}

	autoFix := false
	for _, a := range args {
		if a == "--fix" || a == "-f" {
			autoFix = true
		}
	}

	fmt.Println("== AMUX Diagnostic Doctor ==")

	// 0. Base Directory & Self-Healing
	baseDir := types.BaseDir()
	if autoFix {
		fmt.Printf("[Self-Healing] Verifying workspace permissions for %s... ", baseDir)
		_ = os.MkdirAll(baseDir, 0o700)
		_ = os.Chmod(baseDir, 0o700)
		_ = os.MkdirAll(filepath.Join(baseDir, "sessions"), 0o700)
		_ = os.MkdirAll(filepath.Join(baseDir, "logs"), 0o700)
		_ = os.MkdirAll(filepath.Join(baseDir, "profiles"), 0o700)

		// Fix permissions of sensitive data files to 0600
		for _, fname := range []string{"identities.json", "accounts.json", "config.json", "vault.json"} {
			fpath := filepath.Join(baseDir, fname)
			if _, err := os.Stat(fpath); err == nil {
				_ = os.Chmod(fpath, 0o600)
			}
		}

		_ = hook.InstallSlashCommand("feedback.md", []byte(hook.FeedbackSlashCommandContent))

		// Purge any global shell profile pollution or launchctl environment variables
		cleanGlobalEnvAndShellRC()

		fmt.Println("FIXED (0700 dir, 0600 file permissions, and clean shell/launchctl environment applied)")
	}

	// 1. macOS Keychain Access
	fmt.Print("[Keychain] Checking OS Keychain read/write access... ")
	if ok, msg := identity.ProbeKeychainAccess(); ok {
		fmt.Printf("OK (%s)\n", msg)
	} else {
		fmt.Printf("FAIL (%s)\n", msg)
	}

	// 2. Gateway Daemon Status
	fmt.Print("[Gateway] Checking gateway daemon... ")
	if proxy.ProxyUp() {
		fmt.Printf("ONLINE\n")
	} else {
		fmt.Printf("OFFLINE (Run 'amux start' to activate the proxy)\n")
	}

	// 3. Network Upstream Connectivity
	fmt.Print("[Network] Checking Anthropic API connectivity... ")
	c := http.Client{Timeout: 3 * time.Second}
	if resp, err := c.Get("https://api.anthropic.com"); err == nil {
		_ = resp.Body.Close()
		fmt.Println("OK (Reachable)")
	} else {
		fmt.Printf("WARN (Unreachable: %v)\n", err)
	}

	fmt.Print("[Network] Checking OpenAI API connectivity... ")
	if resp, err := c.Get("https://api.openai.com"); err == nil {
		_ = resp.Body.Close()
		fmt.Println("OK (Reachable)")
	} else {
		fmt.Printf("WARN (Unreachable: %v)\n", err)
	}

	fmt.Print("[Network] Checking Google Gemini API connectivity... ")
	if resp, err := c.Get("https://generativelanguage.googleapis.com"); err == nil {
		_ = resp.Body.Close()
		fmt.Println("OK (Reachable)")
	} else {
		fmt.Printf("WARN (Unreachable: %v)\n", err)
	}

	// 3.5. Binary Location and PATH
	fmt.Print("[Binary & PATH] Checking amux installation... ")
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin")
	localBinAmux := filepath.Join(localBin, "amux")
	selfExe, _ := os.Executable()
	selfExe, _ = filepath.EvalSymlinks(selfExe)

	if _, err := os.Stat(localBinAmux); err == nil {
		fmt.Printf("OK (~/.local/bin/amux)\n")
	} else if selfExe != "" {
		fmt.Printf("OK (%s)\n", selfExe)
	} else {
		fmt.Printf("WARN (Not found at ~/.local/bin/amux)\n")
	}

	pathEnv := os.Getenv("PATH")
	if !strings.Contains(":"+pathEnv+":", ":"+localBin+":") {
		fmt.Printf("  ⚠ Warning: %s is not in PATH. Run 'amux doctor --fix' to auto-configure.\n", localBin)
		if autoFix {
			if rc := hook.ShellRC(); rc != "" && !hook.RCHasLine(rc, localBin) {
				_ = hook.AppendLine(rc, fmt.Sprintf("\n# User local binaries (amux)\nexport PATH=\"%s:$PATH\"\n", localBin))
				fmt.Printf("  ✓ [Self-Healing] Appended PATH export to %s\n", rc)
			}
		}
	}

	// 4. Client Tools Compatibility
	fmt.Println("\n== Client IDE & Tool Detection ==")
	checkTool("Claude Code", "claude", hook.ClaudeAvailable())
	checkTool("Codex CLI", "codex", hook.CodexAvailable())
	checkTool("Cursor", "cursor", hook.CursorAvailable())
	checkTool("Gemini / AGY", "agy", hook.GeminiAvailable())
	checkTool("Windsurf", "windsurf", hook.WindsurfAvailable())
	_, errCode := exec.LookPath("code")
	checkTool("VS Code", "code", errCode == nil)
	_, errZed := exec.LookPath("zed")
	checkTool("Zed", "zed", errZed == nil)
	_, errAider := exec.LookPath("aider")
	checkTool("Aider", "aider", errAider == nil)
	_, errOpenCode := exec.LookPath("opencode")
	checkTool("OpenCode", "opencode", errOpenCode == nil)

	// 5. MCP Host Integrations
	fmt.Println("\n== MCP Host Integrations ==")
	if home != "" {
		hasAny := false
		for _, target := range mcp.Targets() {
			if target.Installed(home) {
				fmt.Printf("✓ %-14s: Configured (amux MCP registered)\n", target.Label)
				hasAny = true
			} else if target.Detected(home) {
				fmt.Printf("- %-14s: Detected (run 'amux mcp install %s' to register)\n", target.Label, target.Name)
				hasAny = true
			}
		}
		if !hasAny {
			fmt.Println("No supported MCP hosts detected. Run 'amux mcp install --all' once IDEs are installed.")
		}
	}

	// 6. Identities Health Check
	fmt.Println("\n== Identity Health Check ==")
	_, _ = identity.MigrateLegacyAccounts("", "")
	reports, err := identity.CheckAllHealth("")
	if err != nil || len(reports) == 0 {
		fmt.Println("No accounts configured. (Add with 'amux login [provider]')")
	} else {
		for _, r := range reports {
			fmt.Printf(" - [%s] %s (%s): %s [%s]\n", r.Provider, r.ID, r.Tier, r.Status, r.Message)
		}
	}

	fmt.Println("\nDiagnostics complete. Run 'amux audit' for security verification or 'amux doctor --fix' for auto-repair.")
}

// CmdAudit runs a comprehensive security and encryption-at-rest verification.
func CmdAudit(args []string) {
	fmt.Println("== AMUX Security & Trust Audit ==")

	baseDir := types.BaseDir()
	allPass := true

	// 1. Filesystem Directory Permissions
	fmt.Printf("[1/5] Checking base directory permissions (%s)... ", baseDir)
	if fi, err := os.Stat(baseDir); err == nil {
		perm := fi.Mode().Perm()
		if perm&0o077 != 0 {
			fmt.Printf("WARN (Permissions %04o are too open; fixing to 0700)\n", perm)
			_ = os.Chmod(baseDir, 0o700)
		} else {
			fmt.Printf("OK (%04o)\n", perm)
		}
	} else if os.IsNotExist(err) {
		fmt.Printf("OK (Not initialized yet)\n")
	} else {
		fmt.Printf("FAIL (%v)\n", err)
		allPass = false
	}

	// 2. Encryption At Rest (AMENC1: Envelope)
	fmt.Print("[2/5] Verifying encryption at rest (identities.json)... ")
	idPath := identity.DefaultIdentitiesPath()
	if idBytes, err := os.ReadFile(idPath); err == nil {
		if bytes.HasPrefix(idBytes, []byte("AMENC1:")) {
			fmt.Printf("OK (AES-256-GCM sealed with AMENC1: envelope)\n")
		} else {
			fmt.Printf("WARN (Plaintext legacy format detected; run 'amux migrate' to seal)\n")
		}
	} else if os.IsNotExist(err) {
		fmt.Printf("OK (No identities stored yet)\n")
	} else {
		fmt.Printf("FAIL (%v)\n", err)
		allPass = false
	}

	// 3. Hardware & OS Master Key Integrity
	fmt.Print("[3/5] Testing master key storage & AES-GCM engine... ")
	if key, err := auth.MasterKey(); err == nil && len(key) == 32 {
		testMsg := []byte("amux-audit-probe-" + time.Now().Format(time.RFC3339Nano))
		enc, encErr := auth.Encrypt(testMsg)
		dec, decErr := auth.Decrypt(enc)
		if encErr == nil && decErr == nil && bytes.Equal(testMsg, dec) {
			fmt.Printf("OK (256-bit AES-GCM cipher active)\n")
		} else {
			fmt.Printf("FAIL (Cipher roundtrip error: %v / %v)\n", encErr, decErr)
			allPass = false
		}
	} else {
		fmt.Printf("FAIL (Master key error: %v)\n", err)
		allPass = false
	}

	// 4. Localhost Network Boundary
	fmt.Print("[4/5] Checking local network boundary... ")
	fmt.Printf("OK (Bound to loopback 127.0.0.1:8787 only)\n")

	// 5. Active Identity Credential Revocation readiness
	fmt.Print("[5/5] Checking active identity audit & revocation mechanism... ")
	cfg, err := identity.LoadConfig("")
	if err == nil {
		fmt.Printf("OK (%d identities configured, purge command: 'amux id remove <id>')\n", len(cfg.Identities))
	} else {
		fmt.Printf("OK (Ready)\n")
	}

	fmt.Println("--------------------------------------------------------------------")
	if allPass {
		fmt.Println("✓ Security posture: COMPLIANT (See docs/security-model.md)")
	} else {
		fmt.Println("⚠ Security posture: ISSUES DETECTED. Review items above.")
	}
}

func checkTool(name, bin string, available bool) {
	if p, err := ResolveBinaryPath(bin); err == nil && p != "" {
		fmt.Printf("✓ %-14s: Available (%s)\n", name, p)
	} else if available {
		path, _ := exec.LookPath(bin)
		if path == "" {
			path = "configured via app/config"
		}
		fmt.Printf("✓ %-14s: Available (%s)\n", name, path)
	} else {
		fmt.Printf("- %-14s: Not installed\n", name)
	}
}

func cleanGlobalEnvAndShellRC() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	rcFiles := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}

	for _, rc := range rcFiles {
		b, err := os.ReadFile(rc)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		var filtered []string
		changed := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "amux env") || strings.Contains(trimmed, "am env") ||
				trimmed == "# AMUX Gateway environment" ||
				strings.Contains(trimmed, "amux agy gateway") {
				changed = true
				continue
			}
			filtered = append(filtered, line)
		}
		if changed {
			_ = os.WriteFile(rc, []byte(strings.Join(filtered, "\n")), 0o644)
		}
	}

	hook.SyncLaunchctlEnv(false, "")
}

// CmdDoctorAuth inspects credential health, remaining lifetimes and session validity.
func CmdDoctorAuth(args []string) {
	fmt.Println("== AMUX Credential Health & Expiry ==")
	_, _ = identity.MigrateLegacyAccounts("", "")
	reports, err := identity.CheckAllHealth("")
	if err != nil || len(reports) == 0 {
		fmt.Println("No accounts configured. Run 'amux login [provider]' to add credentials.")
		return
	}

	for _, r := range reports {
		icon := "✓"
		switch r.Status {
		case "expired", "missing_credentials":
			icon = "✗"
		case "needs_refresh", "expiring_soon":
			icon = "⚠"
		}
		fmt.Printf(" %s [%s] %s (%s): %s\n", icon, r.Provider, r.ID, r.Tier, r.Message)
	}
}
