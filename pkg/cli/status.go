package cli

import (
	"fmt"
	"strings"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/identity"
)

// CmdStatus prints the gateway state, which tools are hooked, and every account.
func CmdStatus(args []string) {
	gw := gateway.GetStatus()
	if gw.Running {
		fmt.Printf("Gateway:  running on %s (pid %d)\n", gw.URL, gw.PID)
	} else {
		fmt.Println("Gateway:  stopped")
	}

	var hooks []string
	for _, h := range []struct {
		name   string
		hooked bool
	}{{"Claude Code", gw.ClaudeHooked}, {"Cursor", gw.CursorHooked}, {"Codex", gw.CodexHooked}, {"Antigravity", gw.AgyHooked}} {
		if h.hooked {
			hooks = append(hooks, h.name)
		}
	}
	if len(hooks) == 0 {
		fmt.Println("Hooked:   none — every tool talks to its own provider directly")
	} else {
		fmt.Printf("Hooked:   %s → gateway\n", strings.Join(hooks, ", "))
		if !gw.Running {
			fmt.Println("  ⚠ These tools point at a gateway that is not running. Run `amux start`, or `amux off` to go back to native.")
		}
	}

	fmt.Println()
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No accounts yet. Add one with: amux login")
		return
	}
	printIdentityTable(cfg)
	fmt.Println("\nPOOL=yes: amux may switch to it automatically. Subscriptions join only via `amux pool add <id>`.")
}
