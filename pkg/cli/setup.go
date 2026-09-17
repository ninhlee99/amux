package cli

import (
	"fmt"

	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
)

// CmdSetup runs the guided setup wizard.
func CmdSetup(args []string) {
	fmt.Println("== AMUX Zero-Touch Runtime & Gateway Setup ==")
	fmt.Println("1. AMUX operates non-invasively by default.")
	fmt.Println("   Your IDEs (Claude Code, Codex, Cursor) read native credentials from macOS Keychain.")
	fmt.Println("2. When subscription accounts reach capacity (95% multi-account / 100% single-account),")
	fmt.Println("   AMUX rotates credentials silently in the Keychain or conditionally activates the Gateway.")

	autoUpdate := false
	for _, a := range args {
		if a == "--auto-update" || a == "-u" {
			autoUpdate = true
		}
	}

	fmt.Println("\n== Probing macOS Keychain Access ==")
	if ok, msg := identity.ProbeKeychainAccess(); ok {
		fmt.Printf("✓ %s\n", msg)
	} else {
		fmt.Printf("⚠ %s\n", msg)
	}

	fmt.Println("\n== Installing /am:feedback Slash Command ==")
	if err := hook.InstallSlashCommand("feedback.md", []byte(hook.FeedbackSlashCommandContent)); err != nil {
		fmt.Printf("Notice: %v\n", err)
	} else {
		fmt.Println("✓ Feedback slash command installed.")
	}

	if autoUpdate {
		fmt.Println("\n== Setting up Auto-Update (LaunchAgent) ==")
		if err := hook.SetupAutoUpdate(true); err != nil {
			fmt.Printf("Auto-update error: %v\n", err)
		} else {
			fmt.Println("✓ Auto-update enabled via LaunchAgent.")
		}
	}

	fmt.Println("\nSetup complete! You can run your IDEs normally (e.g. `claude`, `codex`, `cursor`).")
	fmt.Println("Manage identities with: `amux id list` or `amux id add [provider]`.")
}
