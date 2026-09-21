package cli

import "fmt"

// hasHelp reports whether the argument slice contains -h, --help, or help.
func hasHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}

func usageHelp() {
	fmt.Print(`amux — Thin AI Gateway & AI Developer Infrastructure

Usage: amux <command> [arguments]

Gateway Daemon:
  start [-f] [-p]            Start gateway daemon (-f foreground, -p public 0.0.0.0:8787)
  stop                       Gracefully stop gateway background daemon
  restart                    Restart gateway background daemon
  status                     Real-time dashboard of accounts, quotas & gateway state
  hook [flags]               Hook IDE settings to gateway (--claude, --cursor, --codex, --agy, --all)
  unhook [flags]             Restore IDE settings to direct upstream
  env                        Output shell export statements (eval "$(amux env)")

Account Management:
  login [provider]           Interactive or CLI login (claude, codex, antigravity, gemini, chatgpt, api)
  account list               List all configured accounts, tiers, active status & quotas
  account switch [id]        Switch active account (updates native Keychain & IDE configs)
  use <id>                   Bind current workspace/repo to a specific account (.amux)
  project [status|set|clear] Manage per-project identity and settings
  account logout <id>        Remove account and purge its cached credentials
  account on / off <id>      Enable or temporarily disable an account from failover
  account auto <id> [on|off] Toggle automatic rotation eligibility for an account
  account threshold [id]     View or set account failover quota threshold (default: 95%)
  account health             Probe credential validity and rate-limit limits

Diagnostics & Security:
  doctor [--security]        Run diagnostics on keychain, network, tools & gateway
  audit                      Verify security posture, AES-256-GCM encryption & permissions
  vault [export|import|info] Passphrase-encrypted backup & multi-machine account sync
  usage [day|week|month]     Token usage & request analytics (day, week, month)
  completion [bash|zsh|fish] Generate shell autocompletion script
  setup                      Guided configuration wizard
  config [property]          Inspect or edit configuration (e.g. amux config threshold 85)
  threshold [percent]        Get or set global multi-account failover threshold
  migrate                    Non-destructive migration to flat Identity model
  update [--force]           Update amux binary to latest version
  uninstall [--purge]        Uninstall amux and clean up hooks

Examples:
  amux login claude          Authenticate Claude Code subscription via OAuth
  amux start                 Start AMUX Gateway service in background
  amux account list          Display all configured accounts & status
  amux switch claude:code:01 Switch active account to claude:code:01
  amux status                Inspect real-time gateway routing & quota health
  amux hook --all            Direct all IDE tools through AMUX Gateway

Run 'amux <command> --help' for detailed documentation and options for any command.
`)
}

func helpStart() {
	fmt.Print(`Purpose:
  Start the AMUX AI Gateway background daemon or foreground server.

Usage:
  amux start [options]

Options:
  -f, --foreground    Run gateway in foreground (logs directly to stdout)
  -p, --public        Bind to 0.0.0.0:8787 and require bearer token authentication
  -h, --help          Show this help message

Examples:
  amux start                  # Start gateway detached in background (http://127.0.0.1:8787)
  amux start -f               # Run in foreground for live inspection and debugging
  amux start --public         # Run in public mode with token authentication for LAN/remote

Error Recovery:
  - If port 8787 is in use: Run 'amux restart' or check conflicting processes with 'lsof -i :8787'.
  - To view logs: Run 'tail -f ~/.amux/amux.log'.
`)
}

func helpStop() {
	fmt.Print(`Purpose:
  Gracefully terminate the running AMUX Gateway daemon.

Usage:
  amux stop [options]

Options:
  -p, --public        Reset public binding configuration and revoke public bearer token
  -h, --help          Show this help message

Examples:
  amux stop                   # Stop running gateway background daemon
  amux stop --public          # Stop gateway and reset public network binding

Error Recovery:
  - If daemon is unresponsive: Kill PID directly via 'kill $(cat ~/.amux/gateway.pid)'.
`)
}

func helpRestart() {
	fmt.Print(`Purpose:
  Stop and immediately restart the AMUX Gateway daemon with fresh state.

Usage:
  amux restart [options]

Options:
  -f, --foreground    Restart gateway in foreground
  -p, --public        Restart gateway in public 0.0.0.0:8787 mode
  -h, --help          Show this help message

Examples:
  amux restart                # Restart gateway daemon in background
  amux restart -f             # Restart in foreground mode

Error Recovery:
  - Verify gateway health after restart by running 'amux status' or 'amux doctor'.
`)
}

func helpStatus() {
	fmt.Print(`Purpose:
  Display real-time dashboard of gateway socket, active accounts, quotas & routing modes.

Usage:
  amux status [options]

Options:
  -h, --help          Show this help message

Examples:
  amux status                 # Print full runtime dashboard and identity health
`)
}

func helpLogin() {
	fmt.Print(`Purpose:
  Authenticate and register AI provider accounts (Claude Code, Codex, Antigravity, Gemini, ChatGPT).

Usage:
  amux login [provider] [options]

Arguments:
  [provider]          Provider to authenticate: claude, codex, antigravity, gemini, chatgpt, api

Options:
  --device, -d        Use device/remote authorization code flow (ideal for SSH/headless)
  --manual, -m        Manual OAuth authorization code entry
  --token <key>       Provide API key or access token directly
  --cookie <cookie>   Session cookie for web accounts
  --name <id>         Custom identifier for this account
  -h, --help          Show this help message

Examples:
  amux login claude           # Launch browser for Claude Code OAuth PKCE login
  amux login claude --device  # Device code flow for headless/SSH servers
  amux login codex            # Authenticate OpenAI Codex CLI
  amux login antigravity      # Snapshot active Antigravity IDE login
  amux login api --name deepseek --base-url https://api.deepseek.com --token sk-...

Error Recovery:
  - If browser fails to open: Copy the printed OAuth authorization URL and paste into any browser.
  - If token expires: Run 'amux switch <id>' to trigger automatic token refresh.
`)
}

func helpSwitch() {
	fmt.Print(`Purpose:
  Switch the active account for a provider, updating OS Keychain and IDE configs without re-login.

Usage:
  amux switch [account-id]

Arguments:
  [account-id]        Target account ID (e.g. claude:code:01, antigravity:02). Omit for interactive menu.

Options:
  -h, --help          Show this help message

Examples:
  amux switch                 # Interactive picker showing all configured accounts
  amux switch claude:code:02  # Instantly switch active Claude Code subscription
  amux switch antigravity:01  # Switch to Antigravity account 1

Error Recovery:
  - If account is not found: Run 'amux account list' to view available IDs.
  - If account was disabled: Run 'amux account on <id>' to re-enable it.
`)
}

func helpAccount() {
	fmt.Print(`Purpose:
  Manage AI accounts, identity pools, quotas, and OS Keychain integration.

Usage:
  amux account <subcommand> [arguments]

Subcommands:
  list, ls                    List all accounts, emails, quotas, active state & auto-switch
  switch, select <id>         Switch active account (updates native Keychain & IDE configs)
  login, add [provider]       Add / authenticate a new account
  logout, remove <id>         Remove an account and delete its profile bundle
  on, enable <id>             Re-enable a previously disabled account
  off, disable <id>           Temporarily disable an account from failover rotation
  auto <id> [on|off]          Toggle automatic rotation eligibility for an account
  threshold [id] [val]        Get or set failover quota threshold percentage
  health                      Probe token validity and quota limits across all accounts
  -h, --help                  Show this help message

Examples:
  amux account list           # Display all accounts and active pointers
  amux account switch 01      # Switch to claude:code:01
  amux account auto agy:01 on # Enable automatic failover rotation for an account
  amux account threshold 90   # Set failover threshold to 90%
`)
}

func helpHook() {
	fmt.Print(`Purpose:
  Inject AMUX Gateway endpoint into client IDE configurations for live failover and session routing.

Usage:
  amux hook [flags]

Flags:
  --claude            Hook Claude Code (~/.claude/settings.json)
  --cursor            Hook Cursor IDE
  --codex             Hook Codex CLI
  --agy               Hook Antigravity CLI
  --all               Hook all detected IDEs
  -h, --help          Show this help message

Examples:
  amux hook --all             # Direct all client IDE traffic through AMUX Gateway
  amux hook --claude          # Hook only Claude Code
`)
}

func helpUnhook() {
	fmt.Print(`Purpose:
  Remove AMUX Gateway endpoint from client IDE configurations and restore direct upstream execution.

Usage:
  amux unhook [flags]

Flags:
  --claude            Unhook Claude Code (~/.claude/settings.json)
  --cursor            Unhook Cursor IDE
  --codex             Unhook Codex CLI
  --agy               Unhook Antigravity CLI
  --all               Unhook all detected IDEs
  -h, --help          Show this help message

Examples:
  amux unhook --all           # Restore direct native execution across all IDEs
  amux unhook --claude        # Restore direct native execution for Claude Code
`)
}

func helpDoctor() {
	fmt.Print(`Purpose:
  Run comprehensive health checks on OS Keychain, network connectivity, IDE tools & daemon.

Usage:
  amux doctor [options]

Options:
  -h, --help          Show this help message

Examples:
  amux doctor                 # Run complete system diagnostics
`)
}

func helpUsage() {
	fmt.Print(`Purpose:
  Inspect token usage analytics, cost estimations, and request metrics across all accounts.

Usage:
  amux usage [day|week|month] [date]

Arguments:
  day [YYYY-MM-DD]    Per-account breakdown for a specific date (defaults to today)
  week [YYYY-MM-DD]   Daily breakdown across the week (defaults to current week)
  month [YYYY-MM]     Weekly summary & daily breakdown for the month

Options:
  -h, --help          Show this help message

Examples:
  amux usage day              # View today's token usage across all accounts
  amux usage week             # View current week's token breakdown
  amux usage month 2026-09    # View September 2026 usage analytics
`)
}

func helpEnv() {
	fmt.Print(`Purpose:
  Inspect or output shell environment exports for AMUX Gateway integration.

Usage:
  amux env [subcommand]

Shell Integration:
  Add to ~/.zshrc or ~/.bashrc:
    eval "$(amux env)"

Subcommands:
  (no args)           Output shell export lines (eval "$(amux env)")
  set KEY VALUE       Set a persistent custom environment variable in ~/.amux/env.json
  get KEY             Read a custom environment variable
  rm KEY              Remove a custom environment variable
  list                List all custom environment variables
`)
}

