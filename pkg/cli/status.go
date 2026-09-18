package cli

import (
	"fmt"
	"strings"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/identity"
)

// CmdStatus displays a real-time dashboard of identities, quotas, and gateway state.
func CmdStatus(args []string) {
	fmt.Println("====================== AMUX RUNTIME DASHBOARD ======================")

	// 1. Gateway Status
	gwStatus := gateway.GetStatus()
	gwState := "STOPPED (Zero-Touch Native Mode)"
	if gwStatus.Running {
		gwState = fmt.Sprintf("RUNNING on %s (PID: %d, Sessions: %d)", gwStatus.URL, gwStatus.PID, gwStatus.Sessions)
	}
	fmt.Printf("Gateway Daemon: %s\n", gwState)

	hooks := []string{}
	if gwStatus.ClaudeHooked {
		hooks = append(hooks, "Claude Code")
	}
	if gwStatus.CursorHooked {
		hooks = append(hooks, "Cursor")
	}
	if gwStatus.CodexHooked {
		hooks = append(hooks, "Codex")
	}
	if gwStatus.AgyHooked {
		hooks = append(hooks, "Antigravity")
	}
	if len(hooks) == 0 {
		fmt.Println("Active IDE Hooks: None (Native Direct Keychain Mode)")
	} else {
		fmt.Printf("Active IDE Hooks: %s -> Proxy :8787\n", strings.Join(hooks, ", "))
	}

	// 2. Identities & Quotas
	fmt.Println("\n--------------------------- IDENTITIES -----------------------------")
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No identities configured yet. Run 'amux id add' to add an identity.")
		fmt.Println("====================================================================")
		return
	}

	fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s %-12s\n", "ID", "EMAIL", "THRESHOLD", "USAGE", "ACTIVE", "AUTO-SWITCH", "RESETS IN")
	fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s %-12s\n", "--------------------", "--------------------------", "--------------", "--------", "--------", "------------", "------------")

	for _, id := range cfg.Identities {
		activeStr := "NO"
		if id.Active {
			activeStr = "YES *"
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
		resetStr := id.FormatResetTime()
		thresh := identity.GetAccountThreshold(id, cfg.Identities, cfg.ThresholdPct)
		threshStr := fmt.Sprintf("%.1f%%", thresh)

		fmt.Printf("%-20s %-26s %-14s %-8s %-8s %-12s %-12s\n",
			id.ID, id.Email(), threshStr, usageStr, activeStr, autoStr, resetStr)
	}

	fmt.Println("--------------------------------------------------------------------")
	providers := []string{"anthropic", "gemini", "openai"}
	var threshParts []string
	for _, p := range providers {
		eff := identity.GetEffectiveThreshold(p, cfg.Identities, cfg.ThresholdPct)
		threshParts = append(threshParts, fmt.Sprintf("%s: %.1f%%", p, eff))
	}
	fmt.Printf("Failover Rule: Multi-Account Threshold: %.1f%% | Effective: [%s]\n",
		cfg.ThresholdPct, strings.Join(threshParts, " | "))
	fmt.Println("====================================================================")
}
