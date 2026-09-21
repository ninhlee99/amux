package cli

import (
	"fmt"
	"strings"
	"time"

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

	if strings.HasPrefix(sub, "-") {
		cmdGatewayStart(args)
		return
	}

	switch sub {
	case "start":
		cmdGatewayStart(subArgs)
	case "stop":
		cmdGatewayStop(subArgs)
	case "restart":
		cmdGatewayRestart(subArgs)
	case "status":
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
		die("unknown gateway command: %s (valid: start, stop, status, hook, unhook, token)", sub)
	}
}

func cmdGatewayStart(args []string) {
	if gateway.IsRunning() {
		fmt.Println("Gateway is already running.")
		return
	}

	public := false
	foreground := false
	for _, a := range args {
		if a == "--public" || a == "-p" || a == "public" {
			public = true
		}
		if a == "--foreground" || a == "-f" || a == "foreground" {
			foreground = true
		}
		// -d / --daemon is supported (default mode)
	}

	if foreground {
		if public {
			if err := proxy.SaveBindPublic(true); err != nil {
				die("enable public bind: %v", err)
			}
			tok, err := proxy.LoadOrCreateAuthToken()
			if err != nil {
				die("load/create auth token: %v", err)
			}
			fmt.Println("Starting AMUX Gateway in foreground (PUBLIC mode 0.0.0.0:8787)...")
			fmt.Printf("Access Token: %s\n", tok)
			cmdGatewayRunDaemon()
			return
		}
		fmt.Println("Starting AMUX Gateway in foreground on http://127.0.0.1:8787...")
		cmdGatewayRunDaemon()
		return
	}

	if public {
		if err := proxy.SaveBindPublic(true); err != nil {
			die("enable public bind: %v", err)
		}
		tok, err := proxy.LoadOrCreateAuthToken()
		if err != nil {
			die("load/create auth token: %v", err)
		}
		fmt.Println("Starting detached AMUX Gateway background service (PUBLIC mode 0.0.0.0:8787)...")
		if err := gateway.Start(); err != nil {
			die("failed to start gateway: %v", err)
		}
		if actual, err := proxy.LoadAuthToken(); err == nil && actual != "" {
			tok = actual
		}
		fmt.Printf("✓ Gateway started successfully in PUBLIC mode (0.0.0.0:8787).\n")
		fmt.Printf("  Access Token: %s\n", tok)
		fmt.Printf("  Clients connect with header: 'Authorization: Bearer %s'\n", tok)
		return
	}

	fmt.Println("Starting detached AMUX Gateway background service...")
	if err := gateway.Start(); err != nil {
		die("failed to start gateway: %v", err)
	}
	fmt.Println("✓ Gateway started successfully on http://127.0.0.1:8787 (local only)")
}

func cmdGatewayStop(args []string) {
	publicReset := false
	for _, a := range args {
		if a == "--public" || a == "-p" || a == "public" {
			publicReset = true
		}
	}

	if publicReset {
		_ = proxy.SaveBindPublic(false)
		_ = proxy.ClearAuthToken()
		fmt.Println("Public gateway disabled. Reverted bind to 127.0.0.1 and cleared token.")
	}

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

func cmdGatewayRestart(args []string) {
	if gateway.IsRunning() {
		fmt.Println("Stopping running AMUX Gateway daemon...")
		cmdGatewayStop(nil)
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("Starting AMUX Gateway daemon...")
	cmdGatewayStart(args)
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

	// Public gateway status
	if proxy.IsPublic() {
		tok, _ := proxy.LoadAuthToken()
		fmt.Printf("Public Mode:  ENABLED (Listening on 0.0.0.0:8787)\n")
		if tok != "" {
			fmt.Printf("Access Token: %s\n", tok)
		} else {
			fmt.Printf("Access Token: none configured (run 'amux gateway token new')\n")
		}
	} else {
		fmt.Printf("Public Mode:  DISABLED (127.0.0.1 local only)\n")
	}

	fmt.Println("\n== IDE Settings Hooks ==")
	fmt.Printf("Claude Code:  %s\n", hookStatusStr(st.ClaudeHooked))
	fmt.Printf("Cursor:       %s\n", hookStatusStr(st.CursorHooked))
	fmt.Printf("Codex CLI:    %s\n", hookStatusStr(st.CodexHooked))
	fmt.Printf("Antigravity:  %s\n", hookStatusStr(st.AgyHooked))
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
		case "--agy", "agy", "--antigravity", "antigravity":
			target = gateway.TargetAgy
		case "--all", "-a", "all":
			target = gateway.TargetAll
		default:
			die("unknown hook target: %s (use --claude, --cursor, --codex, --agy, or --all)", args[0])
		}
	}

	if err := gateway.Hook(target, ""); err != nil {
		die("hook error: %v", err)
	}
	fmt.Printf("✓ Injected gateway hook into %s settings.\n", target)
	if !gateway.IsRunning() {
		fmt.Println("Notice: AMUX Gateway daemon is currently STOPPED.")
		fmt.Println("  Run 'amux start' to start the gateway background daemon, or run 'amux unhook' to restore direct upstream connection.")
	}
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
		case "--agy", "agy", "--antigravity", "antigravity":
			target = gateway.TargetAgy
		case "--all", "-a", "all":
			target = gateway.TargetAll
		default:
			die("unknown unhook target: %s (use --claude, --cursor, --codex, --agy, or --all)", args[0])
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
	if len(args) > 0 {
		switch args[0] {
		case "new", "generate":
			tok, err := proxy.IssueNewAuthToken()
			if err != nil {
				die("generate token: %v", err)
			}
			fmt.Printf("Issued new gateway token: %s\n", tok)
			return
		case "clear", "rm", "delete":
			if err := proxy.ClearAuthToken(); err != nil {
				die("clear token: %v", err)
			}
			fmt.Println("✓ Gateway token cleared.")
			return
		}
	}
	tok, err := proxy.LoadAuthToken()
	if err != nil || tok == "" {
		fmt.Println("No active public token configured. Run 'amux gateway token new' to issue one.")
		return
	}
	fmt.Printf("Active gateway token: %s\n", tok)
}
