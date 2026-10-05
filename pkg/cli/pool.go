package cli

import (
	"fmt"
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
	verb := "add"
	if !in {
		verb = "remove"
	}
	if len(args) == 0 {
		die("usage: amux pool %s <id>   (see ids with: amux account list)", verb)
	}
	for _, arg := range args {
		target, err := identity.Get("", arg)
		if err != nil || target == nil {
			die("account %q not found (see ids with: amux account list)", arg)
		}
		if err := identity.SetAutoRotate("", target.ID, in); err != nil {
			die("update %s: %v", target.ID, err)
		}
		if in {
			fmt.Printf("✓ %s added to the pool — amux may switch to it when another pooled account hits its limit.\n", target.ID)
			if !identity.IsEnabled(*target) {
				fmt.Printf("  Note: it is turned off; it takes part once on again (amux account on %s).\n", target.ID)
			}
		} else {
			fmt.Printf("✓ %s removed from the pool — used only when you switch to it (amux switch %s).\n", target.ID, target.ID)
		}
	}
	proxy.Sync()
}

func cmdPoolList() {
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
		fmt.Printf("  %-24s %-8s %s\n", id.ID, tierLabel(id), id.Email())
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
		fmt.Printf("  %-24s %-8s %s%s\n", id.ID, tierLabel(id), id.Email(), state)
	}
	fmt.Println("\nAdd: amux pool add <id>    Remove: amux pool remove <id>")
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
	const row = "%-24s %-28s %-5s %-7s %-5s %-7s %-10s %s\n"
	fmt.Printf(row, "ID", "EMAIL", "TYPE", "ACTIVE", "POOL", "USAGE", "THRESHOLD", "RESETS IN")
	for _, id := range cfg.Identities {
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
		fmt.Printf(row, id.ID, id.Email(), tierLabel(id), active, pool,
			fmt.Sprintf("%.0f%%", id.UsagePercent), thresh, id.FormatResetTime())
	}
}
