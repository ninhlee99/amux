package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/ui"
)

// CmdID handles identity management subcommands.
func CmdID(args []string) {
	if len(args) == 0 {
		cmdIDList()
		return
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list", "ls":
		cmdIDList()
	case "add":
		cmdIDAdd(subArgs)
	case "remove", "rm", "delete":
		cmdIDRemove(subArgs)
	case "health":
		cmdIDHealth()
	case "select", "switch", "sw":
		cmdIDSelect(subArgs)
	default:
		die("unknown id command: %s (valid: add, list, remove, health, select)", sub)
	}
}

func cmdIDList() {
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No identities configured. Run 'amux id add [provider]' to register an identity.")
		return
	}

	fmt.Printf("%-20s %-12s %-14s %-14s %-8s %-8s\n", "ID", "PROVIDER", "TIER", "AUTH TYPE", "USAGE", "ACTIVE")
	fmt.Printf("%-20s %-12s %-14s %-14s %-8s %-8s\n", "--------------------", "------------", "--------------", "--------------", "--------", "--------")

	for _, id := range cfg.Identities {
		activeStr := "no"
		if id.Active {
			activeStr = "YES"
		}
		usageStr := fmt.Sprintf("%.1f%%", id.UsagePercent)
		fmt.Printf("%-20s %-12s %-14s %-14s %-8s %-8s\n",
			id.ID, id.Provider, id.Tier, id.AuthType, usageStr, activeStr)
	}
}

func cmdIDAdd(args []string) {
	providerName := "claude"
	if len(args) > 0 {
		providerName = args[0]
	}

	fmt.Printf("Adding identity for provider: %s\n", providerName)
	// Delegate to interactive login / auth picker which supports OAuth, CDP, API key
	ui.CmdLogin([]string{providerName})

	// Trigger non-destructive sync to identities.json
	_, _ = identity.MigrateLegacyAccounts("", "")
}

func cmdIDRemove(args []string) {
	if len(args) == 0 {
		die("usage: amux id remove <id>")
	}
	id := args[0]
	removed, err := identity.Remove("", id)
	if err != nil {
		die("failed to remove identity %s: %v", id, err)
	}
	if !removed {
		fmt.Printf("Identity %q not found.\n", id)
		return
	}
	fmt.Printf("Identity %q removed from storage.\n", id)
}

func cmdIDHealth() {
	reports, err := identity.CheckAllHealth("")
	if err != nil {
		die("health check failed: %v", err)
	}
	if len(reports) == 0 {
		fmt.Println("No identities configured.")
		return
	}

	fmt.Printf("%-20s %-12s %-12s %-16s %s\n", "ID", "PROVIDER", "TIER", "STATUS", "DETAILS")
	fmt.Printf("%-20s %-12s %-12s %-16s %s\n", "--------------------", "------------", "------------", "----------------", "------------------------------")

	for _, r := range reports {
		fmt.Printf("%-20s %-12s %-12s %-16s %s\n", r.ID, r.Provider, r.Tier, r.Status, r.Message)
	}
}

func cmdIDSelect(args []string) {
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		die("no identities configured")
	}

	var targetID string
	if len(args) > 0 {
		targetID = args[0]
	} else {
		// Interactive picker
		fmt.Println("Select an identity to activate:")
		for i, item := range cfg.Identities {
			activeTag := ""
			if item.Active {
				activeTag = " (current active)"
			}
			fmt.Printf("  [%d] %s (%s, %s)%s\n", i+1, item.ID, item.Provider, item.Tier, activeTag)
		}
		fmt.Print("Enter choice: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		var choice int
		if _, err := fmt.Sscanf(input, "%d", &choice); err == nil && choice >= 1 && choice <= len(cfg.Identities) {
			targetID = cfg.Identities[choice-1].ID
		} else {
			die("invalid selection")
		}
	}

	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		die("identity %q not found", targetID)
	}

	if err := identity.SetActive("", targetID); err != nil {
		die("failed to activate identity: %v", err)
	}

	// Instantly update target IDE native Keychain
	if err := identity.SyncIdentityToNativeKeychain(target); err != nil {
		fmt.Printf("Notice: could not sync to native keychain: %v\n", err)
	} else {
		fmt.Printf("✓ macOS Keychain updated for native %s execution.\n", target.Provider)
	}

	fmt.Printf("✓ Identity %q is now ACTIVE.\n", targetID)
}
