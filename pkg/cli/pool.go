package cli

import (
	"fmt"
	"strconv"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/proxy"
)

// CmdPool manages the rotation pool: the accounts amux may switch between on
// its own when one hits its limit. Subscriptions (Claude Code, Codex,
// Antigravity plans) are never in it until added here.
func CmdPool(args []string) {
	if len(args) == 0 {
		cmdPoolList()
		return
	}
	switch args[0] {
	case "list", "ls":
		cmdPoolList()
	case "add":
		cmdPoolSet(args[1:], true)
	case "remove", "rm", "del", "delete":
		cmdPoolSet(args[1:], false)
	default:
		die("unknown pool command: %s\n  Usage: amux pool [add|remove] <id>", args[0])
	}
}

func cmdPoolSet(args []string, in bool) {
	syncAddressableAdaptersToIdentities()
	verb := "add"
	if !in {
		verb = "remove"
	}
	if len(args) == 0 {
		die("usage: amux pool %s <email|#>   (see accounts with: amux accounts)", verb)
	}
	for _, arg := range args {
		target, err := identity.Get("", arg)
		if err != nil {
			die("%v", err)
		}
		if target == nil {
			die("account %q not found (see accounts with: amux accounts)", arg)
		}
		if err := identity.SetAutoRotate("", target.ID, in); err != nil {
			die("update %s: %v", target.Label(), err)
		}
		if in {
			fmt.Printf("✓ %s added to the pool — amux may switch to it when another pooled account hits its limit.\n", target.Label())
			if !identity.IsEnabled(*target) {
				fmt.Printf("  Note: it is turned off; it takes part once on again (amux account on %s).\n", arg)
			}
		} else {
			fmt.Printf("✓ %s removed from the pool — used only when you switch to it (amux switch %s).\n", target.Label(), arg)
		}
	}
	proxy.Sync()
}

func cmdPoolList() {
	syncAddressableAdaptersToIdentities()
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No accounts yet. Add one with: amux login")
		return
	}
	var in, out []identity.Identity
	for _, id := range cfg.Identities {
		if id.CanAutoRotate() {
			in = append(in, id)
		} else {
			out = append(out, id)
		}
	}
	fmt.Println("In the pool (amux may switch between these automatically):")
	if len(in) == 0 {
		fmt.Println("  (none)")
	}
	for _, id := range in {
		fmt.Printf("  %-4s %-12s %-5s %s\n", "#"+strconv.Itoa(rowNumber(cfg, id)), id.DisplayProvider(), tierLabel(id), accountName(id))
	}
	fmt.Println("\nNot in the pool (used only when you switch to them):")
	if len(out) == 0 {
		fmt.Println("  (none)")
	}
	for _, id := range out {
		state := ""
		if !identity.IsEnabled(id) {
			state = "  (off)"
		}
		fmt.Printf("  %-4s %-12s %-5s %s%s\n", "#"+strconv.Itoa(rowNumber(cfg, id)), id.DisplayProvider(), tierLabel(id), accountName(id), state)
	}
	fmt.Println("\nAdd: amux pool add <email|#>    Remove: amux pool remove <email|#>")
}

// accountName is the ACCOUNT column: the email, or a hint when unknown.
func accountName(id identity.Identity) string {
	if em := id.Email(); em != "" && em != "-" {
		return em
	}
	if id.Tier == identity.TierAPIKey {
		return "(API key)"
	}
	if id.Metadata != nil {
		if prof, ok := id.Metadata["profile_name"].(string); ok && prof != "" {
			return prof
		}
	}
	return "(email unknown)"
}

// accountRef is how a command names an account: its email when that is
// unique, else its row number from `amux accounts`.
func accountRef(cfg *identity.Config, id identity.Identity) string {
	if em := id.Email(); em != "" && em != "-" {
		dup := 0
		for _, it := range cfg.Identities {
			if strings.EqualFold(it.Email(), em) {
				dup++
			}
		}
		if dup == 1 {
			return em
		}
	}
	return strconv.Itoa(rowNumber(cfg, id))
}

// rowNumber is id's 1-based row in `amux accounts` (0 if absent).
func rowNumber(cfg *identity.Config, id identity.Identity) int {
	for i, it := range cfg.Identities {
		if it.ID == id.ID {
			return i + 1
		}
	}
	return 0
}

// tierLabel is the short account type shown in tables.
func tierLabel(id identity.Identity) string {
	switch {
	case id.IsSubscription():
		return "sub"
	case id.Tier == identity.TierWeb:
		return "web"
	case id.Tier == identity.TierAPIKey:
		return "api"
	default:
		return strings.ToLower(string(id.Tier))
	}
}

// printIdentityTable is the account table shared by `amux account list`
// and `amux status`.
func printIdentityTable(cfg *identity.Config) {
	const row = "%-3s %-12s %-30s %-5s %-7s %-5s %-7s %-10s %s\n"
	fmt.Printf(row, "#", "PROVIDER", "ACCOUNT", "TYPE", "ACTIVE", "POOL", "USAGE", "THRESHOLD", "RESETS IN")
	for i, id := range cfg.Identities {
		active := "-"
		switch {
		case !identity.IsEnabled(id):
			active = "off"
		case id.Active:
			active = "yes"
		}
		pool := "no"
		if id.CanAutoRotate() {
			pool = "yes"
		}
		thresh := fmt.Sprintf("%.1f%%", identity.GetAccountThreshold(id, cfg.Identities, cfg.ThresholdPct))
		fmt.Printf(row, strconv.Itoa(i+1), id.DisplayProvider(), accountName(id), tierLabel(id), active, pool,
			fmt.Sprintf("%.0f%%", id.UsagePercent), thresh, id.FormatResetTime())
	}
}
