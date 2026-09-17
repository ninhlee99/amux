package cli

import "fmt"

func usageHelp() {
	fmt.Print(`amux - AI Developer Infrastructure & Universal AI Gateway

Usage: amux <command> [arguments]

Core Commands:
  setup                      Guided setup wizard
  status                     Real-time dashboard of identities, quotas & gateway
  usage [day|week|month]     Token usage & request analytics (day, week, month)
  doctor                     Diagnostics for network, keychain, tools & daemon

Identity Management:
  id add [provider]          Add identity (OAuth, API key, CLI session, or CDP)
  id list                    Display flat accounts, credentials, usage % & auto-switch
  id remove <id>             Delete identity from persistence
  id health                  Probe token validity and remaining quotas
  id select [id]             Manual account switch / OS Keychain rotation
  id auto <id> [on|off]      Toggle auto-rotation (off = manual only, excluded from auto-switch)

Gateway:
  gateway start [--public]   Start detached gateway background service (optional public 0.0.0.0 bind)
  gateway stop [--public]    Gracefully stop the daemon (optional reset public bind)
  gateway status             Inspect socket status, public mode and routing tables
  gateway token [new|clear]  Manage public gateway access token
  gateway hook [flags]       Hook IDE settings (--claude, --cursor, --codex, --all)
  gateway unhook [flags]     Unhook IDE settings (--claude, --cursor, --codex, --all)

Configuration & Migration:
  config                     Inspect or edit configuration
  migrate                    Non-destructive migration to flat Identity model

System:
  update [--force]           Update amux to latest version
  uninstall [--purge]        Uninstall amux and remove hooks
`)
}
