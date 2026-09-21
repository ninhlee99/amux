package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/ui"
)

// CmdAccount handles account and identity management subcommands.
func CmdAccount(args []string) {
	if len(args) == 0 {
		cmdIDList()
		return
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "help", "-h", "--help":
		cmdAccountHelp()
	case "off", "disable":
		cmdIDOff(subArgs)
	case "on", "enable":
		cmdIDOn(subArgs)
	case "list", "ls":
		cmdIDList()
	case "add", "login":
		cmdIDAdd(subArgs)
	case "remove", "rm", "delete", "logout":
		cmdIDRemove(subArgs)
	case "health":
		cmdIDHealth()
	case "select", "switch", "use":
		cmdIDSelect(subArgs)
	case "auto":
		cmdIDAutoRotate(subArgs)
	case "threshold":
		cmdIDThreshold(subArgs)
	default:
		die("unknown account command: %s (valid: list, switch, add, remove, logout, on, off, auto, threshold, health)", sub)
	}
}

// CmdID is an alias for CmdAccount for backward compatibility.
func CmdID(args []string) {
	CmdAccount(args)
}

func cmdAccountHelp() {
	fmt.Print(`Usage: amux account <subcommand> [arguments]

Manage accounts, identities, and session credentials across AI providers.

Subcommands:
  list, ls                   List all accounts, emails, tiers, active status, and quotas
  switch, select <id>        Switch active account (updates native Keychain & IDE configs)
  login, add [provider]      Add / authenticate a new account (claude, agy, gemini, codex, api)
  logout, remove <id>        Remove an account and delete its profile bundle
  on, enable <id>            Re-enable a previously disabled account
  off, disable <id>          Temporarily disable an account from failover rotation
  auto <id> [on|off]         Toggle automatic rotation eligibility for an account
  threshold [id] [val]       Get or set failover threshold percentage
  health                     Probe token validity and quota health across all accounts

Examples:
  amux account list
  amux account switch claude:code:01
  amux account login claude
  amux account auto antigravity:01 on
  amux account threshold 90.0
`)
}

func cmdIDList() {
	_, _ = identity.MigrateLegacyAccounts("", "")
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No identities configured. Run 'amux id add [provider]' to register an identity.")
		return
	}

	fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n", "ID", "EMAIL", "MODEL", "THRESHOLD", "USAGE", "ACTIVE", "AUTO-SWITCH", "RESETS IN")
	fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n", "--------------------", "--------------------------", "------------------", "--------------", "--------", "--------", "------------", "------------")

	for _, id := range cfg.Identities {
		activeStr := "NO"
		if id.Active {
			activeStr = "YES"
		}
		if !identity.IsEnabled(id) {
			activeStr = "DISABLED"
		}
		autoStr := "ON"
		if !id.CanAutoRotate() {
			autoStr = "OFF"
		}
		if !identity.IsEnabled(id) {
			autoStr = "-"
		}
		usageStr := fmt.Sprintf("%.1f%%", id.UsagePercent)
		thresh := identity.GetAccountThreshold(id, cfg.Identities, cfg.ThresholdPct)
		threshStr := fmt.Sprintf("%.1f%%", thresh)
		resetStr := "unknown"
		if id.ResetAt > 0 {
			resetStr = id.FormatResetTime()
		}
		fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n",
			id.ID, id.Email(), id.ModelName(), threshStr, usageStr, activeStr, autoStr, resetStr)
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
	target, _ := identity.Get("", id)
	profDeleted, _ := profile.DeleteProfileAnyTool(id)
	if target != nil && target.Metadata != nil {
		if pName, ok := target.Metadata["profile_name"].(string); ok && pName != "" && pName != id {
			deleted, _ := profile.DeleteProfileAnyTool(pName)
			if deleted {
				profDeleted = true
			}
		}
	}
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
		die("no identities configured.\n  Run 'amux login [claude|codex|antigravity|gemini]' to authenticate an account.")
	}

	var targetID string
	if len(args) > 0 {
		arg := strings.TrimSpace(args[0])
		// Check if user specified 1-based index (e.g. 'amux switch 1')
		if num, err := strconv.Atoi(arg); err == nil && num >= 1 && num <= len(cfg.Identities) {
			targetID = cfg.Identities[num-1].ID
		} else {
			targetID = arg
		}
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
			die("invalid selection. Please enter a valid number from the list above.")
		}
	}

	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		var available []string
		for _, id := range cfg.Identities {
			available = append(available, id.ID)
		}
		die("identity %q not found.\n  Available identities: %s\n  (Run 'amux account list' to inspect all accounts)", targetID, strings.Join(available, ", "))
	}

	tool := ""
	switch {
	case strings.HasPrefix(target.ID, "antigravity"):
		tool = "antigravity"
	case strings.HasPrefix(target.ID, "claude"):
		tool = "claude"
	case strings.HasPrefix(target.ID, "gemini"):
		tool = "gemini"
	case strings.HasPrefix(target.ID, "codex"):
		tool = "codex"
	}

	// 1. Snapshot the CURRENT active account BEFORE touching keychain or config files
	if tool != "" {
		profile.SyncActiveFromSystem(tool)
	}

	pName := ""
	if target.Metadata != nil {
		if s, ok := target.Metadata["profile_name"].(string); ok && s != "" {
			pName = s
		}
	}
	if pName == "" && target.Email() != "" && target.Email() != "-" {
		pName = target.Email()
	}

	// 2. If backed by an amux profile bundle, restore it (refreshes expired token if needed & installs)
	if tool != "" && pName != "" {
		if _, err := os.Stat(profile.BundlePath(tool, pName)); err == nil {
			if err := profile.CmdUse(tool, pName); err != nil {
				fmt.Printf("Notice: profile restore: %v\n", err)
			}
		}
	}

	// 3. Update target IDE native Keychain & configs
	if err := identity.SyncIdentityToNativeKeychain(target); err != nil {
		fmt.Printf("Notice: could not sync to native credentials: %v\n", err)
	} else {
		fmt.Printf("✓ Native credentials updated for %s execution.\n", target.Provider)
	}

	// 4. Update active pointer in identity store
	if err := identity.SetActive("", target.ID); err != nil {
		die("failed to activate identity: %v", err)
	}

	// 5. Notify proxy daemon
	if proxy.ProxyUp() {
		proxy.Sync()
		if tool == "claude" && pName != "" {
			proxy.CmdSwitch("claude", pName)
		} else {
			proxy.CmdSwitchProvider(target.ID)
		}
	}

	fmt.Printf("✓ Account %q is now ACTIVE.\n", target.ID)
	fmt.Println("  💡 Active terminal sessions or IDEs will pick up this identity on their next command or restart.")
}

func cmdIDThreshold(args []string) {
	cfg, err := identity.LoadConfig("")
	if err != nil {
		die("load config: %v", err)
	}

	if len(args) == 0 {
		fmt.Printf("Global multi-account threshold: %.1f%%\n\n", cfg.ThresholdPct)
		if len(cfg.Identities) == 0 {
			fmt.Println("No identities configured.")
			return
		}
		fmt.Printf("%-20s %-12s %-14s %s\n", "ID", "PROVIDER", "THRESHOLD", "TYPE")
		fmt.Printf("%-20s %-12s %-14s %s\n", "--------------------", "------------", "--------------", "------")
		for _, id := range cfg.Identities {
			thresh := identity.GetAccountThreshold(id, cfg.Identities, cfg.ThresholdPct)
			source := "(default)"
			if id.ThresholdPct != nil {
				source = "(custom)"
			}
			fmt.Printf("%-20s %-12s %-14s %s\n", id.ID, id.Provider, fmt.Sprintf("%.1f%%", thresh), source)
		}
		return
	}

	// 1 argument: can be a global threshold value (number) or identity ID to inspect
	if len(args) == 1 {
		valStr := strings.TrimSuffix(args[0], "%")
		// Check if it's a number (global threshold update, e.g. 'amux id threshold 80')
		if val, err := strconv.ParseFloat(valStr, 64); err == nil && val > 0 && val <= 100 {
			cfg.ThresholdPct = val
			if err := identity.SaveConfig("", cfg); err != nil {
				die("save config: %v", err)
			}
			proxy.Sync()
			fmt.Printf("✓ Updated global threshold_pct to %.1f%%\n", val)
			return
		}

		// Otherwise inspect identity threshold
		targetID := args[0]
		target, err := identity.Get("", targetID)
		if err != nil || target == nil {
			die("identity %q not found (or invalid threshold number 0-100)", targetID)
		}
		thresh := identity.GetAccountThreshold(*target, cfg.Identities, cfg.ThresholdPct)
		customStr := "(effective default)"
		if target.ThresholdPct != nil {
			customStr = "(custom override)"
		}
		fmt.Printf("Threshold for %q (%s): %.1f%% %s\n", target.ID, target.Provider, thresh, customStr)
		return
	}

	// 2+ arguments:
	// 'amux id threshold global <val>' OR 'amux id threshold <id> <val>'
	first := args[0]
	second := strings.TrimSuffix(args[1], "%")

	if strings.EqualFold(first, "global") || strings.EqualFold(first, "--global") {
		val, err := strconv.ParseFloat(second, 64)
		if err != nil || val <= 0 || val > 100 {
			die("invalid threshold value (must be 0-100)")
		}
		cfg.ThresholdPct = val
		if err := identity.SaveConfig("", cfg); err != nil {
			die("save config: %v", err)
		}
		proxy.Sync()
		fmt.Printf("✓ Updated global threshold_pct to %.1f%%\n", val)
		return
	}

	targetID := first
	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		die("identity %q not found", targetID)
	}

	if strings.EqualFold(second, "reset") || strings.EqualFold(second, "default") || strings.EqualFold(second, "clear") || second == "0" {
		if err := identity.SetThreshold("", target.ID, nil); err != nil {
			die("failed to update threshold: %v", err)
		}
		proxy.Sync()
		fmt.Printf("✓ Reset threshold for %q to default/effective\n", target.ID)
		return
	}

	val, err := strconv.ParseFloat(second, 64)
	if err != nil || val <= 0 || val > 100 {
		die("invalid threshold value %q (must be between 0 and 100)", args[1])
	}

	if err := identity.SetThreshold("", target.ID, &val); err != nil {
		die("failed to update threshold: %v", err)
	}
	proxy.Sync()
	fmt.Printf("✓ Updated threshold for %q to %.1f%%\n", target.ID, val)
}

// cmdIDOff hard-disables an identity so it is never used by the proxy
// (rotator + pool) until explicitly re-enabled with `amux id on`.
// For Claude subscription accounts (claude:code:XX) it also marks the
// underlying profile bundle as disabled so the rotator respects it immediately.
func cmdIDOff(args []string) {
	if len(args) == 0 {
		die("usage: amux id off <id>")
	}
	targetID := args[0]
	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		die("identity %q not found", targetID)
	}

	// 1. Hard-disable in identity store
	if err := identity.SetEnabled("", targetID, false); err != nil {
		die("failed to disable identity: %v", err)
	}

	// 2. For Claude subscription profiles: also mark the profile bundle disabled
	//    so the rotator (which reads profile metadata, not identities.json) picks
	//    it up immediately via RefreshFromDisk called during proxy.Sync().
	if strings.HasPrefix(target.ID, "claude:code") {
		pName := ""
		if target.Metadata != nil {
			if s, ok := target.Metadata["profile_name"].(string); ok {
				pName = s
			}
		}
		if pName == "" {
			pName = target.Email()
		}
		if pName != "" && pName != "-" {
			_ = profile.SetDisabled("claude", pName, true)
		}
	}

	proxy.Sync()
	fmt.Printf("✓ Identity %q is now OFF — will not be used until `amux id on %s`.\n", targetID, targetID)
}

// cmdIDOn re-enables a hard-disabled identity.
func cmdIDOn(args []string) {
	if len(args) == 0 {
		die("usage: amux id on <id>")
	}
	targetID := args[0]
	target, err := identity.Get("", targetID)
	if err != nil || target == nil {
		die("identity %q not found", targetID)
	}

	// 1. Re-enable in identity store
	if err := identity.SetEnabled("", targetID, true); err != nil {
		die("failed to enable identity: %v", err)
	}

	// 2. For Claude subscription profiles: clear the profile bundle disabled flag
	if strings.HasPrefix(target.ID, "claude:code") {
		pName := ""
		if target.Metadata != nil {
			if s, ok := target.Metadata["profile_name"].(string); ok {
				pName = s
			}
		}
		if pName == "" {
			pName = target.Email()
		}
		if pName != "" && pName != "-" {
			_ = profile.SetDisabled("claude", pName, false)
		}
	}

	proxy.Sync()
	fmt.Printf("✓ Identity %q is now ON — back in routing pool.\n", targetID)
}

