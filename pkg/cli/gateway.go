package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/hook"
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
	case "_supervise":
		// Internal entry point run by `amux start`: keeps the daemon alive.
		cmdGatewaySupervise()
	case "_daemon":
		// Internal entry point run by detached daemon process
		cmdGatewayRunDaemon()
	case "token":
		cmdGatewayToken(subArgs)
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
	fmt.Println("✓ Gateway stopped.")
	if hooked := hookedTools(); len(hooked) > 0 {
		fmt.Printf("  ⚠ Still hooked: %s — they will fail until `amux start`.\n", strings.Join(hooked, ", "))
		fmt.Println("    To go back to native instead: amux off")
	}
}

// hookedTools lists the tools whose config currently points at the gateway.
func hookedTools() []string {
	st := gateway.GetStatus()
	var out []string
	if st.ClaudeHooked {
		out = append(out, "Claude Code")
	}
	if st.CursorHooked {
		out = append(out, "Cursor")
	}
	if st.CodexHooked {
		out = append(out, "Codex")
	}
	if st.AgyHooked {
		out = append(out, "Antigravity")
	}
	return out
}

// CmdOff returns every tool to native mode: removes all gateway hooks and
// stops the gateway. If an account ID is provided, it disables that account only.
func CmdOff(args []string) {
	if len(args) > 0 {
		cmdIDOff(args)
		return
	}
	hooked := hookedTools()
	if err := gateway.Unhook(gateway.TargetAll); err != nil {
		die("unhook: %v", err)
	}
	cleanGlobalEnvAndShellRC()
	if len(hooked) > 0 {
		fmt.Printf("✓ Unhooked: %s\n", strings.Join(hooked, ", "))
	}
	wasRunning := gateway.IsRunning()
	if wasRunning {
		if err := gateway.Stop(); err != nil {
			die("stop gateway: %v", err)
		}
		fmt.Println("✓ Gateway stopped.")
	}
	if len(hooked) == 0 && !wasRunning {
		fmt.Println("amux is already off — no tool is hooked and the gateway is not running.")
		return
	}
	fmt.Println("✓ amux is off — every tool uses its own login directly. Restart open sessions to pick this up.")
}

// CmdOn re-enables a turned-off account in rotation, or starts the gateway if no args given.
func CmdOn(args []string) {
	if len(args) > 0 {
		cmdIDOn(args)
		return
	}
	cmdGatewayStart(args)
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
	fmt.Printf("Windsurf:     %s\n", func() string {
		if hook.WindsurfAvailable() {
			return "AVAILABLE (Launch via 'amux run windsurf')"
		}
		return "UNHOOKED (Launch via 'amux run windsurf')"
	}())
}

func hookStatusStr(hooked bool) string {
	if hooked {
		return "HOOKED (Routing via Gateway :8787)"
	}
	return "UNHOOKED (Direct native execution)"
}

func cmdGatewayHook(args []string) {
	if len(args) == 0 || args[0] == "status" || args[0] == "list" || args[0] == "ls" {
		cmdGatewayHookStatus()
		return
	}

	// Session hooks from older versions run `amux hook <tool> start|stop`
	// when a session opens or closes. Hooking is an explicit user action
	// now, so those calls must not re-hook a tool the user unhooked.
	if len(args) > 1 && (args[1] == "start" || args[1] == "stop") {
		return
	}

	target := gateway.TargetAll
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
		die("unknown hook target: %s\n\nUsage:\n  amux hook [target]\n\nTargets:\n  claude, cursor, codex, agy, all\n\nOr run 'amux hook' to view current interception status.", args[0])
	}

	if err := gateway.Hook(target, ""); err != nil {
		die("hook error: %v", err)
	}
	fmt.Printf("✓ Hooked %s → gateway. Undo with: amux unhook %s\n", target, target)
	if !gateway.IsRunning() {
		fmt.Println("  ⚠ The gateway is not running, so hooked tools will fail. Run: amux start")
	}
}

func cmdGatewayHookStatus() {
	st := gateway.GetStatus()
	fmt.Println("== AMUX Client Hook & Interception Status ==")
	fmt.Println()

	type clientInfo struct {
		Name     string
		Detected bool
		Hooked   bool
		Config   string
	}

	clients := []clientInfo{
		{
			Name:     "Claude Code",
			Detected: hook.ClaudeAvailable(),
			Hooked:   st.ClaudeHooked,
			Config:   gateway.ClaudeSettingsPath(),
		},
		{
			Name:     "Cursor IDE",
			Detected: hook.CursorAvailable(),
			Hooked:   st.CursorHooked,
			Config:   gateway.CursorSettingsPath(),
		},
		{
			Name:     "Codex CLI",
			Detected: hook.CodexAvailable(),
			Hooked:   st.CodexHooked,
			Config:   gateway.CodexTomlPath(),
		},
		{
			Name:     "Antigravity (AGY)",
			Detected: hook.GeminiAvailable(),
			Hooked:   st.AgyHooked,
			Config:   gateway.AGYSettingsPath(),
		},
		{
			Name:     "Windsurf",
			Detected: hook.WindsurfAvailable(),
			Hooked:   false,
			Config:   "Settings > Models > OpenAI Base URL / amux run windsurf",
		},
	}

	for _, c := range clients {
		detStr := "Found"
		if !c.Detected {
			detStr = "Not installed"
		}
		statusStr := "DIRECT (Native execution)"
		if c.Hooked {
			statusStr = "HOOKED (Routing via Gateway :8787)"
		}
		fmt.Printf("• %-18s [%s] -> %s\n", c.Name, detStr, statusStr)
		fmt.Printf("  Config: %s\n", c.Config)
	}

	fmt.Println()
	fmt.Println("Actionable Commands:")
	fmt.Println("  amux hook --all      Hook all detected AI IDEs/CLIs to AMUX Gateway")
	fmt.Println("  amux hook claude     Hook only Claude Code")
	fmt.Println("  amux unhook --all    Restore native direct connection for all tools")
	if !st.Running {
		fmt.Println("  amux start           Start AMUX background proxy daemon (:8787)")
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

	before := hookedTools()
	if err := gateway.Unhook(target); err != nil {
		die("unhook error: %v", err)
	}
	after := map[string]bool{}
	for _, t := range hookedTools() {
		after[t] = true
	}
	var removed []string
	for _, t := range before {
		if !after[t] {
			removed = append(removed, t)
		}
	}
	if len(removed) == 0 {
		fmt.Println("Nothing to unhook — no tool points at the gateway.")
		return
	}
	fmt.Printf("✓ Unhooked %s — back to native. Restart open sessions to pick this up.\n", strings.Join(removed, ", "))
}

func cmdGatewayRunDaemon() {
	_ = gateway.RunDaemon(func() error {
		return proxy.RunProxy("", "")
	})
}

func cmdGatewaySupervise() {
	self, err := os.Executable()
	if err != nil {
		die("find self binary: %v", err)
	}
	pidPath := gateway.SupervisorPIDFilePath()
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o755)
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
	defer os.Remove(pidPath)
	_ = proxy.RunSupervisor(self, "gateway", "_daemon")
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
