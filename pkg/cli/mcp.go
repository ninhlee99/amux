package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"amux-accounts/pkg/mcp"
)

const mcpInstructions = `amux connects this agent to the user's other AI chat accounts (ChatGPT, Claude, Gemini web sessions, subscriptions and API keys).
- amux_providers: list available accounts & models in the pool
- amux_ask: send self-contained prompts to any account or family with auto-failover
- amux_review: request an independent expert code review on diffs/patches
- amux_diagnose: analyze errors, crashes, and stack traces with root-cause identification
- amux_fix: generate precise code fixes and patches
- amux_analyze: evaluate project architecture, schemas, and trade-offs
- amux_status: check gateway connectivity and active provider routes`

// NewMCPServer builds the stdio MCP server with every amux tool registered.
func NewMCPServer() *mcp.Server {
	s := mcp.NewServer("amux", amuxVersion)
	s.Instructions = mcpInstructions
	s.Logger = log.New(os.Stderr, "amux-mcp: ", log.LstdFlags)
	mcp.RegisterAmuxTools(s, &mcp.PoolBackend{})
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
	if err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		lower := strings.ToLower(exe)
		isTemp := strings.Contains(lower, "go-build") || strings.Contains(lower, "/tmp/") || strings.Contains(lower, "\\temp\\") || strings.Contains(lower, "var/folders")
		if !isTemp && filepath.Base(exe) == "amux" {
			return exe
		}
	}

	// Try finding amux from PATH
	if p, err := exec.LookPath("amux"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			if r, err := filepath.EvalSymlinks(abs); err == nil {
				return r
			}
			return abs
		}
		return p
	}

	// Fallback to standard installation paths
	if home, err := os.UserHomeDir(); err == nil {
		for _, cand := range []string{
			filepath.Join(home, ".local", "bin", "amux"),
			filepath.Join(home, "bin", "amux"),
			filepath.Join(home, "go", "bin", "amux"),
			"/usr/local/bin/amux",
			"/opt/homebrew/bin/amux",
		} {
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}
	}

	if exe != "" {
		lower := strings.ToLower(exe)
		if !strings.Contains(lower, "go-build") && !strings.Contains(lower, "/tmp/") && !strings.Contains(lower, "var/folders") {
			return exe
		}
	}
	return "amux"
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
  your chat accounts (ChatGPT / Claude / Gemini web, subscriptions, API keys) as tools.

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
  amux_providers, amux_ask, amux_review, amux_diagnose, amux_fix, amux_analyze, amux_status

Examples:
  amux mcp install claude cursor
  amux mcp install all
`, strings.Join(mcp.TargetNames(), ", "))
}
