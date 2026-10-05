package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/identity"
)

// CmdDashboard launches the interactive TUI Dashboard for AMUX.
func CmdDashboard(args []string) {
	jsonMode := false
	onceMode := false

	for _, arg := range args {
		switch arg {
		case "--json", "-j":
			jsonMode = true
		case "--once", "-1", "-o":
			onceMode = true
		}
	}

	if jsonMode {
		renderDashboardJSON()
		return
	}

	if onceMode {
		renderDashboardScreen()
		return
	}

	// Interactive TUI loop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	clearScreen()
	renderDashboardScreen()

	for {
		select {
		case <-sigCh:
			fmt.Println()
			return
		case <-ticker.C:
			clearScreen()
			renderDashboardScreen()
		}
	}
}

func renderDashboardScreen() {
	gw := gateway.GetStatus()

	fmt.Println("\033[1;36m┌─────────────────────────────────────────────────────────────┐\033[0m")
	fmt.Println("\033[1;36m│                 AMUX AI GATEWAY DASHBOARD                   │\033[0m")
	fmt.Println("\033[1;36m└─────────────────────────────────────────────────────────────┘\033[0m")

	// Gateway Status Section
	statusText := "\033[1;31mSTOPPED\033[0m"
	if gw.Running {
		statusText = fmt.Sprintf("\033[1;32mONLINE (%s - PID %d)\033[0m", gw.URL, gw.PID)
	}
	fmt.Printf(" \033[1mGateway Status:\033[0m %s\n", statusText)
	fmt.Printf(" \033[1mLocal Time:\033[0m     %s\n\n", time.Now().Format("15:04:05 Mon 02 Jan 2006"))

	// Accounts & Pool Section
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println(" \033[33mNo accounts configured. Run `amux login` to connect.\033[0m")
	} else {
		fmt.Println(" \033[1;4mActive Accounts & Pool Status:\033[0m")
		fmt.Printf(" %-16s %-12s %-10s %-10s\n", "ID / BRAND", "METHOD", "IN POOL", "ACTIVE")
		fmt.Println(" " + strings.Repeat("─", 52))

		for _, id := range cfg.Identities {
			poolStatus := "\033[32mYES\033[0m"
			if !id.CanAutoRotate() {
				poolStatus = "\033[90mNO\033[0m"
			}
			activeStatus := " "
			if id.Active {
				activeStatus = "\033[1;32m● (Active)\033[0m"
			}

			fmt.Printf(" %-16s %-12s %-10s %-10s\n", id.ID, id.Provider, poolStatus, activeStatus)
		}
		fmt.Println()
	}

	// Supported IDE Agents status
	fmt.Println(" \033[1;4mSupported IDE Coding Agents:\033[0m")
	fmt.Println("  • Claude Code CLI    (anthropic dialect / messages stream)")
	fmt.Println("  • Cursor IDE         (openai dialect / chat completions)")
	fmt.Println("  • Google AGY         (gemini dialect / thought signature)")
	fmt.Println("  • OpenAI Codex CLI   (responses dialect / tool harness)")
	fmt.Println()

	fmt.Println("\033[90m[Auto-refreshing every 2s • Press Ctrl+C to exit]\033[0m")
}

func renderDashboardJSON() {
	gw := gateway.GetStatus()
	cfg, _ := identity.LoadConfig("")

	data := map[string]any{
		"gateway_online": gw.Running,
		"gateway_url":    gw.URL,
		"gateway_pid":    gw.PID,
		"timestamp":      time.Now().Unix(),
		"accounts_count": len(cfg.Identities),
		"accounts":       cfg.Identities,
		"ides":           []string{"Claude Code", "Cursor", "AGY", "Codex"},
	}

	bytes, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(bytes))
}

// Help handler for dashboard
func helpDashboard() {
	fmt.Println("Usage: amux dashboard [flags]")
	fmt.Println("  Launches the interactive TUI Dashboard for AMUX.")
	fmt.Println()
	fmt.Println("  Flags:")
	fmt.Println("    --once, -1, -o    Print a single snapshot and exit")
	fmt.Println("    --json, -j        Output dashboard data in JSON format")
}
