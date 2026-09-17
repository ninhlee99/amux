package cli

import (
	"fmt"
	"strings"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/proxy"
)

// CmdGateway handles gateway lifecycle, hooks, and routing inspection.
func CmdGateway(args []string) {
	if len(args) == 0 {
		cmdGatewayStatus()
		return
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "start", "up":
		cmdGatewayStart(subArgs)
	case "stop", "down":
		cmdGatewayStop(subArgs)
	case "status", "st":
		cmdGatewayStatus()
	case "hook":
		cmdGatewayHook(subArgs)
	case "unhook":
		cmdGatewayUnhook(subArgs)
	case "_daemon":
		// Internal entry point run by detached daemon process
		cmdGatewayRunDaemon()
	case "token":
		cmdGatewayToken(subArgs)
	case "btw":
		if len(subArgs) == 0 {
			die("usage: amux gateway btw <message>")
		}
		proxy.CmdBtw(strings.Join(subArgs, " "))
	default:
		die("unknown gateway command: %s (valid: start, stop, status, hook, unhook)", sub)
	}
}

func cmdGatewayStart(args []string) {
	if gateway.IsRunning() {
		fmt.Println("Gateway is already running.")
		return
	}
	fmt.Println("Starting detached AMUX Gateway background service...")
	if err := gateway.Start(); err != nil {
		die("failed to start gateway: %v", err)
	}
	fmt.Println("✓ Gateway started successfully on http://127.0.0.1:8787")
}

func cmdGatewayStop(args []string) {
	if !gateway.IsRunning() {
		fmt.Println("Gateway is not running.")
		return
	}
	fmt.Println("Stopping AMUX Gateway...")
	if err := gateway.Stop(); err != nil {
		die("failed to stop gateway: %v", err)
	}
	fmt.Println("✓ Gateway stopped gracefully.")
}

func cmdGatewayStatus() {
	st := gateway.GetStatus()
	fmt.Println("== AMUX Universal AI Gateway ==")
	if st.Running {
		fmt.Printf("Status:       RUNNING\n")
		fmt.Printf("Endpoint:     %s\n", st.URL)
		fmt.Printf("PID:          %d\n", st.PID)
		fmt.Printf("Sessions:     %d\n", st.Sessions)
		if st.Upstream != "" {
			fmt.Printf("Upstream:     %s\n", st.Upstream)
		}
	} else {
		fmt.Printf("Status:       STOPPED (Direct native Keychain execution)\n")
		fmt.Printf("Default URL:  %s\n", st.URL)
	}

	fmt.Println("\n== IDE Settings Hooks ==")
	fmt.Printf("Claude Code:  %s\n", hookStatusStr(st.ClaudeHooked))
	fmt.Printf("Cursor:       %s\n", hookStatusStr(st.CursorHooked))
	fmt.Printf("Codex CLI:    %s\n", hookStatusStr(st.CodexHooked))
}

func hookStatusStr(hooked bool) string {
	if hooked {
		return "HOOKED (Routing via Gateway :8787)"
	}
	return "UNHOOKED (Direct native execution)"
}

func cmdGatewayHook(args []string) {
	target := gateway.TargetAll
	if len(args) > 0 {
		switch args[0] {
		case "--claude", "-c", "claude":
			target = gateway.TargetClaude
		case "--cursor", "cursor":
			target = gateway.TargetCursor
		case "--codex", "codex":
			target = gateway.TargetCodex
		case "--all", "-a", "all":
			target = gateway.TargetAll
		default:
			die("unknown hook target: %s (use --claude, --cursor, --codex, or --all)", args[0])
		}
	}

	if err := gateway.Hook(target, ""); err != nil {
		die("hook error: %v", err)
	}
	fmt.Printf("✓ Injected gateway hook into %s settings.\n", target)
}

func cmdGatewayUnhook(args []string) {
	target := gateway.TargetAll
	if len(args) > 0 {
		switch args[0] {
		case "--claude", "-c", "claude":
			target = gateway.TargetClaude
		case "--cursor", "cursor":
			target = gateway.TargetCursor
		case "--codex", "codex":
			target = gateway.TargetCodex
		case "--all", "-a", "all":
			target = gateway.TargetAll
		default:
			die("unknown unhook target: %s (use --claude, --cursor, --codex, or --all)", args[0])
		}
	}

	if err := gateway.Unhook(target); err != nil {
		die("unhook error: %v", err)
	}
	fmt.Printf("✓ Removed gateway hook from %s settings (restored native execution).\n", target)
}

func cmdGatewayRunDaemon() {
	_ = gateway.RunDaemon(func() error {
		return proxy.RunProxy("", "")
	})
}

func cmdGatewayToken(args []string) {
	if len(args) > 0 && args[0] == "new" {
		tok, err := proxy.IssueNewAuthToken()
		if err != nil {
			die("generate token: %v", err)
		}
		fmt.Printf("Issued new gateway token: %s\n", tok)
		return
	}
	tok, err := proxy.LoadAuthToken()
	if err != nil || tok == "" {
		fmt.Println("No active public token configured. Run 'amux gateway token new' to issue one.")
		return
	}
	fmt.Printf("Active gateway token: %s\n", tok)
}
