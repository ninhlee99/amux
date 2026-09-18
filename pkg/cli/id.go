package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
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
	case "list":
		cmdIDList()
	case "add":
		cmdIDAdd(subArgs)
	case "remove":
		cmdIDRemove(subArgs)
	case "health":
		cmdIDHealth()
	case "select":
		cmdIDSelect(subArgs)
	case "auto":
		cmdIDAutoRotate(subArgs)
	default:
		die("unknown id command: %s (valid: list, add, remove, select, auto, health)", sub)
	}
}

func cmdIDList() {
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		if n, _ := identity.MigrateLegacyAccounts("", ""); n > 0 {
			cfg, _ = identity.LoadConfig("")
		}
	}
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No identities configured. Run 'amux id add [provider]' to register an identity.")
		return
	}

	fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s\n", "ID", "EMAIL", "TIER", "USAGE", "ACTIVE", "AUTO-SWITCH")
	fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s\n", "--------------------", "--------------------------", "--------------", "--------", "--------", "------------")

	for _, id := range cfg.Identities {
		activeStr := "NO"
		if id.Active {
			activeStr = "YES"
		}
		autoStr := "ON"
		if !id.CanAutoRotate() {
			autoStr = "OFF"
		}
		usageStr := fmt.Sprintf("%.1f%%", id.UsagePercent)
		fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s\n",
			id.ID, id.Email(), id.Tier, usageStr, activeStr, autoStr)
	}
}

func cmdIDAutoRotate(args []string) {
	if len(args) == 0 {
		die("usage: amux id auto <id> [on|off]")
	}
	targetID := args[0]
	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		die("identity %q not found", targetID)
	}

	enabled := !target.CanAutoRotate() // default toggle if no argument
	if len(args) > 1 {
		val := strings.ToLower(args[1])
		switch val {
		case "on", "true", "enable", "1", "yes":
			enabled = true
		case "off", "false", "disable", "0", "no":
			enabled = false
		default:
			die("invalid state: %s (use 'on' or 'off')", args[1])
		}
	}

	if err := identity.SetAutoRotate("", targetID, enabled); err != nil {
		die("failed to update identity: %v", err)
	}
	proxy.Sync()

	if enabled {
		fmt.Printf("✓ AUTO-SWITCH set to ON for %q (eligible for automatic rotation/failover).\n", targetID)
	} else {
		fmt.Printf("✓ AUTO-SWITCH set to OFF for %q (manual switch only via 'amux id select %s').\n", targetID, targetID)
	}
}

func cmdIDAdd(args []string) {
	if len(args) == 0 {
		ui.CmdLogin(nil)
		_, _ = identity.MigrateLegacyAccounts("", "")
		return
	}

	providerName := args[0]
	fmt.Printf("Adding identity for provider: %s\n", providerName)
	ui.CmdLogin(args)
	_, _ = identity.MigrateLegacyAccounts("", "")
}

func cmdIDRemove(args []string) {
	if len(args) == 0 {
		die("usage: amux id remove <id>")
	}
	id := args[0]
	profDeleted, _ := profile.DeleteProfileAnyTool(id)
	provDeleted := provider.RemoveProvider(provider.DefaultAccountsPath(), id) == nil
	removed, err := identity.Remove("", id)
	if err != nil {
		die("failed to remove identity %s: %v", id, err)
	}
	if !removed && !profDeleted && !provDeleted {
		fmt.Printf("Identity %q not found.\n", id)
		return
	}
	proxy.Sync()
	fmt.Printf("✓ Identity %q removed from storage.\n", id)
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
			emailTag := ""
			if em := item.Email(); em != "-" && em != "" {
				emailTag = fmt.Sprintf(" <%s>", em)
			}
			fmt.Printf("  [%d] %s%s (%s, %s)%s\n", i+1, item.ID, emailTag, item.Provider, item.Tier, activeTag)
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
	proxy.Sync()

	// Instantly update target IDE native Keychain
	if err := identity.SyncIdentityToNativeKeychain(target); err != nil {
		fmt.Printf("Notice: could not sync to native keychain: %v\n", err)
	} else {
		fmt.Printf("✓ macOS Keychain updated for native %s execution.\n", target.Provider)
	}

	fmt.Printf("✓ Identity %q is now ACTIVE.\n", targetID)
}
