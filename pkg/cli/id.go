package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
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
		die("unknown account command: %s (valid: list, switch, add, remove, on, off, threshold, health)", sub)
	}
}

// CmdID is an alias for CmdAccount for backward compatibility.
func CmdID(args []string) {
	CmdAccount(args)
}

func cmdAccountHelp() {
	fmt.Print(`Usage: amux account <command> [id]

Commands:
  list                  Show all accounts (default)
  switch <id>           Make <id> the active account (no re-login)
  add [provider]        Log in a new account (same as: amux login)
  remove <id>           Delete an account and its saved login
  off <id>              Turn an account off — never used until turned on
  on <id>               Turn it back on
  threshold [id] [pct]  Usage % at which a pooled account hands over (default 95)
  health                Check every saved login is still valid

Pool membership (automatic switching) is managed with: amux pool

Examples:
  amux account list
  amux account switch claude:code:02
  amux account off codex:01
`)
}

func parseJWTEmail(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return ""
	}
	seg := parts[1]
	if m := len(seg) % 4; m != 0 {
		seg += strings.Repeat("=", 4-m)
	}
	b, err := base64.URLEncoding.DecodeString(seg)
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(b, &claims)
	return strings.TrimSpace(claims.Email)
}

func syncAddressableAdaptersToIdentities() {
	_, _ = identity.MigrateLegacyAccounts("", "")
	adapters, err := provider.LoadAllAddressable(provider.DefaultAccountsPath())
	if err != nil || len(adapters) == 0 {
		return
	}
	cfg, err := identity.LoadConfig("")
	if err != nil || cfg == nil {
		cfg = &identity.Config{ThresholdPct: identity.DefaultThresholdPct}
	}
	existing := make(map[string]bool)
	for _, id := range cfg.Identities {
		existing[id.ID] = true
	}
	changed := false
	for _, a := range adapters {
		if existing[a.ID()] {
			continue
		}
		tier := identity.TierSubscription
		authType := string(identity.AuthOAuth)
		pLower := strings.ToLower(a.ID())
		providerName := "subscription"
		switch {
		case strings.HasPrefix(pLower, "claude"):
			providerName = "claude"
		case strings.HasPrefix(pLower, "codex"):
			providerName = "codex"
		case strings.HasPrefix(pLower, "agy") || strings.HasPrefix(pLower, "antigravity"):
			providerName = "antigravity"
		case strings.HasPrefix(pLower, "chatgpt"):
			providerName = "chatgpt"
			tier = identity.TierWeb
			authType = string(identity.AuthCDP)
		case strings.HasPrefix(pLower, "gemini:web"):
			providerName = "gemini"
			tier = identity.TierWeb
			authType = string(identity.AuthCDP)
		case strings.Contains(pLower, ":api"):
			tier = identity.TierAPIKey
			authType = string(identity.AuthAPIKey)
			providerName = strings.Split(pLower, ":")[0]
		}
		creds := map[string]string{
			"source": "system_adapter",
		}
		email := "-"
		if strings.HasPrefix(pLower, "codex") {
			if doc, err := os.ReadFile(provider.CodexAuthPath()); err == nil {
				var d map[string]any
				if json.Unmarshal(doc, &d) == nil {
					if tokens, ok := d["tokens"].(map[string]any); ok {
						for _, k := range []string{"id_token", "access_token"} {
							if tok, ok := tokens[k].(string); ok && tok != "" {
								if em := parseJWTEmail(tok); em != "" {
									email = em
									break
								}
							}
						}
					}
				}
			}
		}
		meta := map[string]interface{}{
			"migrated_from": "system_adapter",
			"plan":          "pro",
		}
		if email != "" && email != "-" {
			meta["email"] = email
			creds["account"] = email
		}
		newId := identity.Identity{
			ID:          a.ID(),
			Provider:    providerName,
			Tier:        tier,
			AuthType:    authType,
			Credentials: creds,
			Active:      false,
			Metadata:    meta,
		}
		if err := identity.Upsert("", newId); err == nil {
			existing[a.ID()] = true
			changed = true
		}
	}
	if changed {
		proxy.Sync()
	}
}

// Login commands in pkg/ui print this same table when they finish.
func init() { ui.AccountsTable = cmdIDList }

func cmdIDList() {
	syncAddressableAdaptersToIdentities()
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No accounts yet. Add one with: amux login")
		return
	}

	printIdentityTable(cfg)
	fmt.Println("\nSwitch: amux switch <email|#>    Pool: amux pool add|remove <email|#>")
}

func cmdIDAutoRotate(args []string) {
	if len(args) == 0 {
		die("usage: amux pool add|remove <id>")
	}
	targetID := args[0]
	target, err := identity.Get("", targetID)
	if err != nil {
		die("%v", err)
	}
	if target == nil {
		die("account %q not found (see: amux accounts)", targetID)
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
		fmt.Printf("✓ %s added to the pool (same as: amux pool add %s).\n", targetID, targetID)
	} else {
		fmt.Printf("✓ %s removed from the pool (same as: amux pool remove %s).\n", targetID, targetID)
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

// resolveAccountRef turns what the user typed (email, provider:email, row #
// or internal ID) into the internal ID plus a label for messages. An
// ambiguous email dies with the choices; an unknown ref is returned as-is.
func resolveAccountRef(ref string) (id, label string) {
	target, err := identity.Get("", ref)
	if err != nil {
		die("%v", err)
	}
	if target == nil {
		return ref, ref
	}
	return target.ID, target.Label()
}

func cmdIDRemove(args []string) {
	if len(args) == 0 {
		die("usage: amux account remove <email|#>")
	}
	id, label := resolveAccountRef(args[0])
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
		fmt.Printf("Account %q not found (see: amux accounts).\n", args[0])
		return
	}
	proxy.Sync()
	fmt.Printf("✓ %s removed (its saved login was deleted; the tool's current login is untouched).\n", label)
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
	syncAddressableAdaptersToIdentities()
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		die("no accounts yet. Add one with: amux login")
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
		fmt.Println("Which account should be active?")
		for i, item := range cfg.Identities {
			activeTag := ""
			switch {
			case !identity.IsEnabled(item):
				activeTag = " (turned off)"
			case item.Active:
				activeTag = " (current active)"
			}
			fmt.Printf("  [%d] %s — %s (%s)%s\n", i+1, item.DisplayProvider(), accountName(item), tierLabel(item), activeTag)
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
	if err != nil {
		die("%v", err)
	}
	if target == nil {
		var available []string
		for _, id := range cfg.Identities {
			available = append(available, accountRef(cfg, id))
		}
		die("account %q not found. Use an email or row number from `amux accounts`: %s", targetID, strings.Join(available, ", "))
	}

	tool := ""
	switch {
	case strings.HasPrefix(target.ID, "antigravity"), strings.HasPrefix(target.ID, "agy"):
		tool = "antigravity"
	case strings.HasPrefix(target.ID, "claude"):
		tool = "claude"
	case strings.HasPrefix(target.ID, "gemini"):
		tool = "gemini"
	case strings.HasPrefix(target.ID, "codex"):
		tool = "codex"
	}

	if !identity.IsEnabled(*target) {
		fmt.Printf("Account %s was turned off. Re-enabling...\n", target.Label())
		if err := identity.SetEnabled("", target.ID, true); err != nil {
			die("failed to enable %s: %v", target.Label(), err)
		}
		if strings.HasPrefix(target.ID, "claude:code") {
			pName := ""
			if target.Metadata != nil {
				if v, ok := target.Metadata["profile_name"].(string); ok {
					pName = v
				}
			}
			if pName == "" {
				pName = target.Email()
			}
			if pName != "" && pName != "-" {
				_ = profile.SetDisabled("claude", pName, false)
			}
		}
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

	switch {
	case tool != "" && target.IsSubscription():
		// IDE subscription: install its saved login into the CLI's own
		// keychain/config. The bundle is kept fresh by amux, so no re-login.
		if pName != "" {
			if _, err := os.Stat(profile.BundlePath(tool, pName)); err == nil {
				if err := proxy.SwitchProfile(tool, pName); err != nil {
					die("could not switch to %s: %v\n  If its login expired, sign in once more: amux login %s", target.Label(), err, loginNameForTool(tool))
				}
				break
			}
		}
		// Older identities without a saved bundle: write what we have.
		if err := identity.SyncIdentityToNativeKeychain(target); err != nil {
			die("no saved login for %s: %v\n  Sign in once with: amux login %s", target.Label(), err, loginNameForTool(tool))
		}
		fmt.Printf("✓ Native credentials updated for %s.\n", target.Provider)
	case proxy.ProxyUp():
		// Web / API account: make the gateway prefer it.
		proxy.CmdSwitchProvider(target.ID)
	}

	if err := identity.SetActive("", target.ID); err != nil {
		die("failed to activate identity: %v", err)
	}
	proxy.Sync()

	fmt.Printf("✓ %s is now active.\n", target.Label())
	if tool != "" && target.IsSubscription() {
		fmt.Println("  Running sessions keep the old account until restarted (e.g. `claude --continue`).")
	}
}

// loginNameForTool maps a profile tool name to its `amux login` argument.
func loginNameForTool(tool string) string {
	if tool == "antigravity" {
		return "agy"
	}
	return tool
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
		if err != nil {
			die("%v", err)
		}
		if target == nil {
			die("account %q not found (or invalid threshold number 0-100)", targetID)
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
	if err != nil {
		die("%v", err)
	}
	if target == nil {
		die("account %q not found (see: amux accounts)", targetID)
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

// cmdIDOff disables an identity or provider so it is never used by the proxy
// (rotator + pool) until explicitly re-enabled with `amux on <id>`.
func cmdIDOff(args []string) {
	if len(args) == 0 {
		die("usage: amux off <email|#>")
	}
	targetID, label := resolveAccountRef(args[0])
	found := false

	// 1. Check identity store
	if target, err := identity.Get("", targetID); err == nil && target != nil {
		if err := identity.SetEnabled("", targetID, false); err != nil {
			die("failed to disable identity: %v", err)
		}
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
		found = true
	}

	// 2. Also check provider accounts.json
	if err := provider.SetEnabled(provider.DefaultAccountsPath(), targetID, false); err == nil {
		found = true
	}

	if !found {
		die("account %q not found (see: amux accounts)", args[0])
	}

	proxy.Sync()
	fmt.Printf("✓ %s is off — out of rotation (amux on %s)\n", label, args[0])
}

// cmdIDOn re-enables a disabled identity or provider.
func cmdIDOn(args []string) {
	if len(args) == 0 {
		die("usage: amux on <email|#>")
	}
	targetID, label := resolveAccountRef(args[0])
	found := false

	// 1. Check identity store
	if target, err := identity.Get("", targetID); err == nil && target != nil {
		if err := identity.SetEnabled("", targetID, true); err != nil {
			die("failed to enable identity: %v", err)
		}
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
		found = true
	}

	// 2. Also check provider accounts.json
	if err := provider.SetEnabled(provider.DefaultAccountsPath(), targetID, true); err == nil {
		found = true
	}

	if !found {
		die("account %q not found (see: amux accounts)", args[0])
	}

	proxy.Sync()
	fmt.Printf("✓ %s is back in rotation.\n", label)
}

