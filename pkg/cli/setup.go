package cli

import (
	"fmt"

	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/identity"
)

// CmdSetup runs the guided setup wizard.
func CmdSetup(args []string) {
	fmt.Println("== amux setup ==")
	fmt.Println("Your tools keep using their own logins; amux changes nothing until you ask:")
	fmt.Println("  amux switch <id>    change a tool's login")
	fmt.Println("  amux pool add <id>  allow automatic switching for that account")
	fmt.Println("  amux hook <tool>    route a tool through the gateway")

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

	fmt.Println("\n== Installing /amux:feedback Slash Command ==")
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
	fmt.Println("Accounts: amux account list   Add one: amux login")
}
