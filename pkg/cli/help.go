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
	fmt.Print(`amux — use several AI accounts from Claude Code, Codex, Cursor and Antigravity

Usage: amux <command> [args]        Help for one command: amux <command> --help

Accounts
  login [provider]        Add an account: claude, codex, agy, chatgpt, gemini-web
                          or an API key: gemini, groq, kimi, grok, github
  account list            Show all accounts (also: amux ls)
  switch <id>             Make <id> the active login of its tool — no re-login
  account off|on <id>     Never use an account / allow it again
  account remove <id>     Delete an account and its saved login

Pool (automatic switching)
  pool                    Show which accounts amux may switch between on its own
  pool add <id>           Allow automatic switching to <id> (subscriptions are never added for you)
  pool remove <id>        Back to manual-only

Gateway (run IDEs through amux)
  start | stop | restart  Run the local gateway on http://127.0.0.1:8787
  dashboard               Live interactive TUI Dashboard (monitor quota, toggle pool)
  run <ide>               Run an IDE (claude, cursor, codex, agy) in a sandboxed gateway session
  off                     Stop the gateway and ensure all native tools run directly
  status                  Gateway and active accounts at a glance

MCP (use your accounts as tools in any MCP agent)
  mcp install [host|all]  Register amux in Claude Code, Cursor, Codex, Gemini CLI, …
  mcp uninstall [host]    Remove it (no host = everywhere it is registered)
  mcp status              Where amux is registered

Maintenance
  doctor                  Check keychain, tools and gateway
  usage [day|week|month]  Token usage per account
  config secret-store …   Keep secrets in the OS keychain (default) or a local vault file
  update                  Update amux
  uninstall [--purge]     Remove amux cleanly (--purge also deletes ~/.amux)

Examples
  amux login claude && amux login claude     Save two Claude Code accounts
  amux switch claude:code:02                 Use the second one in Claude Code now
  amux pool add claude:code:01 claude:code:02  Let amux switch between them at the limit
`)
}

func helpPool() {
	fmt.Print(`Purpose:
  Choose which accounts amux may switch between on its own.

Usage:
  amux pool                 List accounts in / out of the pool
  amux pool add <id>...     Add accounts to the pool
  amux pool remove <id>...  Take accounts out of the pool

The pool is the set of accounts amux may switch between by itself when one
reaches its limit (default 95%, see: amux account threshold).

  • Subscriptions (Claude Code, Codex, Antigravity plans) are never in the
    pool unless you add them here.
  • amux only switches from a pooled account to another pooled account.
    An account outside the pool is used only when you pick it (amux switch).
  • Web and API-key accounts are in the pool by default; remove them here.

Examples:
  amux pool add claude:code:01 claude:code:02
  amux pool remove codex:01
`)
}

func helpOff() {
	fmt.Print(`Purpose:
  Turn off an account from rotation, or turn off the gateway and unhook all IDEs.

Usage:
  amux off [account-id]

Details:
  • With an account ID: disables the account so it is skipped during rotation.
  • Without arguments: unhooks every tool (Claude Code, Codex, Cursor, Antigravity)
    and stops the gateway, so each tool talks to its own provider directly.

Examples:
  amux off claude:code:01           Disable account from rotation
  amux off                          Back to native (unhook and stop gateway)
`)
}

func helpOn() {
	fmt.Print(`Purpose:
  Re-enable a turned-off account in rotation, or start the gateway daemon.

Usage:
  amux on [account-id]

Details:
  • With an account ID: re-enables the account in rotation.
  • Without arguments: starts the AMUX Gateway daemon.

Examples:
  amux on claude:code:01            Re-enable account in rotation
  amux on                           Start the gateway daemon
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
  Stop the local gateway.

Usage:
  amux stop [--public]

Stops the gateway. Hooked tools keep pointing at it and will fail until
'amux start' — use 'amux off' to unhook them and stop in one step.
--public also turns public mode off and revokes its token.

Examples:
  amux stop
  amux off        Stop and unhook every tool
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
  [provider]          claude, codex, agy, chatgpt, gemini-web (accounts)
                      gemini, groq, kimi, grok, github, cursor (API keys)

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
  amux login groq --token gsk_...

Error Recovery:
  - If browser fails to open: Copy the printed OAuth authorization URL and paste into any browser.
  - Logging in to a tool's account makes it that tool's active login; the previous login is saved
    first, so 'amux switch <id>' brings it back without logging in again.
`)
}

func helpSwitch() {
	fmt.Print(`Purpose:
  Change which saved account a tool is logged in with — no browser login.

Usage:
  amux switch [id]          (no id: pick from a list; a number from that list also works)

Makes <id> the active login of its tool. For Claude Code, Codex and
Antigravity the saved login is written back into the tool's own keychain /
config, so no browser login is needed. The login you are leaving is saved
first, so switching back later works too. For web / API accounts it makes
the gateway prefer that account.

Running sessions keep the old account until restarted (claude --continue).

Examples:
  amux switch claude:code:02
  amux switch codex:01

If it fails: the account's login expired — run amux login <tool> once more.
`)
}

func helpAccount() {
	fmt.Print("Purpose:\n  List, turn on/off and remove accounts.\n\n")
	cmdAccountHelp()
}

func helpHook() {
	fmt.Print(`Purpose:
  Route a tool through the local gateway (optional).

Usage:
  amux hook                 Show which tools are hooked
  amux hook <tool>          claude | codex | cursor | agy | all

Points the tool's config at the local gateway (http://127.0.0.1:8787), which
must be running (amux start). amux only adds its own keys and never touches
the rest of the config. Undo with: amux unhook <tool>

Files changed:
  claude   ~/.claude/settings.json  env.ANTHROPIC_BASE_URL, env.ANTHROPIC_AUTH_TOKEN
  codex    ~/.codex/config.toml     openai_base_url
  cursor   Cursor settings.json     cursor.openaiBaseUrl
  agy      ~/.gemini/antigravity-cli/settings.json, shell rc block, launchctl env

Examples:
  amux hook claude
  amux hook all
`)
}

func helpUnhook() {
	fmt.Print(`Purpose:
  Undo 'amux hook' — the tool talks to its own provider again.

Usage:
  amux unhook [tool]        claude | codex | cursor | agy | all (default: all)

Removes exactly what 'amux hook' added; values you set yourself are kept.
To also stop the gateway in one step: amux off

Examples:
  amux unhook claude
  amux unhook
`)
}

func helpDoctor() {
	fmt.Print(`Purpose:
  Run comprehensive health checks on OS Keychain, network connectivity, IDE tools & daemon.

Usage:
  amux doctor [options]

Options:
  --fix                       Repair permissions and PATH while checking
  --live [--provider <id>]    Send real turns through the gateway (Anthropic,
                              OpenAI streaming, tool call) and name the layer
                              that fails; spends a few tokens
  providers                   Probe each account directly, bypassing the gateway
  --security                  Run the security audit
  -h, --help                  Show this help message

Examples:
  amux doctor                 # Run complete system diagnostics
  amux doctor --live          # End-to-end check of the running gateway
  amux doctor --live --provider chatgpt:01
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
  Inspect or manage custom environment variables for AMUX Gateway integration.
  (Recommended: Use 'amux run <ide>' for clean, sandboxed session execution without polluting shell profiles).

Usage:
  amux env [subcommand]

Subcommands:
  (no args)           Output shell export lines for debugging
  set KEY VALUE       Set a persistent custom environment variable in ~/.amux/env.json
  get KEY             Read a custom environment variable
  rm KEY              Remove a custom environment variable
  list                List all custom environment variables
`)
}

