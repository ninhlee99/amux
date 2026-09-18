package cli

import "fmt"

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

Account Management:
  login [provider]           Interactive or CLI login (claude, codex, antigravity, gemini, chatgpt, api)
  account list               List all configured accounts, tiers, active status & quotas
  account switch [id]        Switch active account (updates native Keychain & IDE configs)
  account logout <id>        Remove account and purge its cached credentials
  account on / off <id>      Enable or temporarily disable an account from failover
  account auto <id> [on|off] Toggle automatic rotation eligibility for an account
  account threshold [id]     View or set account failover quota threshold (default: 95%)
  account health             Probe credential validity and rate-limit limits

Diagnostics & Setup:
  doctor                     Run comprehensive diagnostics on keychain, network, tools & gateway
  usage [day|week|month]     Token usage & request analytics (day, week, month)
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
`)
}
