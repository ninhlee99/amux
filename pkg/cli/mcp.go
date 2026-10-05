package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"amux-accounts/pkg/mcp"
	"amux-accounts/pkg/muse"
)

const mcpInstructions = `amux connects this agent to the user's other AI chat accounts (ChatGPT, Claude, Gemini, Meta Muse web sessions, subscriptions and API keys).
- amux_providers lists accounts; amux_ask sends a self-contained prompt to one (or lets amux pick, with failover). The other AI cannot see your files: include the context it needs.
- muse_* tools drive Meta Muse in the user's logged-in browser profile (chat, attachments, generated images/video). If muse_status says loggedIn=false, call muse_login and ask the user to sign in in the opened window.`

// NewMCPServer builds the stdio MCP server with every amux tool registered.
func NewMCPServer() *mcp.Server {
	s := mcp.NewServer("amux", amuxVersion)
	s.Instructions = mcpInstructions
	s.Logger = log.New(os.Stderr, "amux-mcp: ", log.LstdFlags)
	mcp.RegisterAmuxTools(s, &mcp.PoolBackend{})
	mcp.RegisterMuseTools(s, func() mcp.MuseClient { return muse.Shared(muse.ConfigFromEnv()) })
	return s
}

// CmdMCP implements `amux mcp [serve|install|uninstall|status|config|tools]`.
func CmdMCP(args []string) {
	sub := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "serve", "stdio":
		cmdMCPServe()
	case "install", "add":
		cmdMCPInstall(args, true)
	case "uninstall", "remove", "rm", "off":
		cmdMCPInstall(args, false)
	case "status", "ls", "list":
		cmdMCPStatus()
	case "config", "snippet":
		cmdMCPConfig(args)
	case "tools":
		for _, t := range NewMCPServer().Tools() {
			fmt.Printf("%-16s %s\n", t.Name, t.Title)
		}
	default:
		helpMCP()
	}
}

func cmdMCPServe() {
	// stdout belongs to JSON-RPC: anything else printed by a library would
	// corrupt the stream, so route stray writes to stderr.
	out := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := NewMCPServer()
	if err := s.Serve(ctx, os.Stdin, out); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "amux mcp: %v\n", err)
		os.Exit(1)
	}
}

// amuxBinary is the absolute, symlink-resolved path IDE configs should run.
func amuxBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "amux"
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func mcpTargetsFromArgs(args []string, home string) ([]mcp.Target, bool) {
	all := len(args) == 0
	var targets []mcp.Target
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == "all" {
			all = true
			continue
		}
		t, ok := mcp.FindTarget(a)
		if !ok {
			die("unknown MCP host %q (supported: %s)", a, strings.Join(mcp.TargetNames(), ", "))
		}
		targets = append(targets, t)
	}
	if all {
		targets = targets[:0]
		for _, t := range mcp.Targets() {
			if t.Detected(home) {
				targets = append(targets, t)
			}
		}
	}
	return targets, all
}

func cmdMCPInstall(args []string, install bool) {
	home, _ := os.UserHomeDir()
	bin := amuxBinary()
	targets, all := mcpTargetsFromArgs(args, home)
	if !install && all {
		// Remove from every host amux is registered in, detected or not.
		targets = targets[:0]
		for _, t := range mcp.Targets() {
			if t.Installed(home) {
				targets = append(targets, t)
			}
		}
		if len(targets) == 0 {
			fmt.Println("amux is not registered in any MCP host — nothing to remove.")
			return
		}
	}
	if len(targets) == 0 {
		fmt.Println("No supported MCP hosts detected. Name one explicitly, e.g. `amux mcp install cursor`.")
		return
	}
	failed := 0
	for _, t := range targets {
		var where string
		var err error
		if install {
			where, err = t.Install(home, bin)
		} else {
			where, err = t.Uninstall(home)
		}
		switch {
		case err == mcp.ErrHasComments:
			failed++
			fmt.Printf("  %-15s skipped — %s has comments; add this by hand:\n%s\n", t.Label, t.Path(home), indent(t.Snippet(bin), "      "))
		case err != nil:
			failed++
			fmt.Printf("  %-15s FAILED: %v\n", t.Label, err)
		case install:
			fmt.Printf("  %-15s registered (%s)\n", t.Label, where)
		default:
			fmt.Printf("  %-15s removed (%s)\n", t.Label, where)
		}
	}
	if install {
		fmt.Printf("\nServer: %s mcp — restart the IDE/agent to load the amux tools.\n", bin)
		if all {
			fmt.Println("Hosts not detected were skipped; install one explicitly with `amux mcp install <host>`.")
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func cmdMCPStatus() {
	home, _ := os.UserHomeDir()
	fmt.Println("MCP hosts:")
	for _, t := range mcp.Targets() {
		state := "not detected"
		switch {
		case t.Installed(home):
			state = "registered"
		case t.Detected(home):
			state = "detected, not registered"
		}
		fmt.Printf("  %-15s %-26s %s\n", t.Name, state, t.Path(home))
	}
	fmt.Println("\nRegister: amux mcp install [host…|all]   Remove: amux mcp uninstall [host…]")
}

func cmdMCPConfig(args []string) {
	bin := amuxBinary()
	if len(args) == 0 {
		args = mcp.TargetNames()
	}
	for _, a := range args {
		t, ok := mcp.FindTarget(a)
		if !ok {
			die("unknown MCP host %q (supported: %s)", a, strings.Join(mcp.TargetNames(), ", "))
		}
		home, _ := os.UserHomeDir()
		fmt.Printf("# %s — %s\n%s\n\n", t.Label, t.Path(home), t.Snippet(bin))
	}
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+pad)
}

func helpMCP() {
	fmt.Printf(`Purpose:
  Run amux as a Model Context Protocol server so any MCP-capable coding agent can use
  your chat accounts (ChatGPT / Claude / Gemini / Meta Muse web, subscriptions, API keys) as tools.

Usage:
  amux mcp                       Serve MCP over stdio (what IDE configs run)
  amux mcp install [host…|all]   Register amux in MCP hosts (default: all detected)
  amux mcp uninstall [host…]     Remove the registration (no host: everywhere it is registered)
  amux mcp status                Show which hosts have amux registered
  amux mcp config [host…]        Print the config snippet to paste by hand
  amux mcp tools                 List the tools amux exposes

Hosts:
  %s

Tools:
  amux_providers, amux_ask, amux_status,
  muse_status, muse_login, muse_new_chat, muse_chat, muse_read_last, muse_chats,
  muse_open_chat, muse_read_chat, muse_media, muse_dump_dom, muse_close

Environment:
  AMUX_MUSE_CDP=http://127.0.0.1:9222   Attach to a Chrome you already run (never killed)
  AMUX_MUSE_HEADLESS=1                  Run the Muse browser headless (after first login)
  AMUX_MUSE_PROFILE=<name>              Use another browser profile under ~/.amux/browser-profiles
  AMUX_MUSE_URL=<url>                   Muse app root (default https://muse.ai/)

Examples:
  amux login muse && amux mcp install claude cursor
  amux mcp install all
`, strings.Join(mcp.TargetNames(), ", "))
}
