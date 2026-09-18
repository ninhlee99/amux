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
  id list                    Display flat accounts, emails, usage % & auto-switch state
  id add [provider]          Add an identity (claude, agy, gemini, codex, api, cursor...)
  id remove <id|name>        Delete an identity and profile bundle (e.g. amux id remove solo)
  id select [id]             Manual account switch / OS Keychain rotation
  id auto <id> [on|off]      Toggle auto-rotation (off = manual only, excluded from auto-switch)
  id threshold [id] [val]    Get or set account threshold (or global threshold if no id)
  id health                  Probe token validity and remaining quotas

Gateway:
  gateway start [--public]   Start detached gateway background service (optional public 0.0.0.0 bind)
  gateway stop [--public]    Gracefully stop the daemon (optional reset public bind)
  gateway status             Inspect socket status, public mode and routing tables
  gateway token [new|clear]  Manage public gateway access token
  gateway hook [flags]       Hook IDE settings (--claude, --cursor, --codex, --all)
  gateway unhook [flags]     Unhook IDE settings (--claude, --cursor, --codex, --all)

Configuration & Migration:
  config [property]          Inspect or edit configuration (e.g. amux config threshold 85)
  threshold [percent]        Get or set global multi-account failover threshold (default: 95.0)
  migrate                    Non-destructive migration to flat Identity model

System:
  feedback [-b|-i] [title]   File a GitHub issue (bug or feature request)
  update [--force]           Update amux to latest version
  uninstall [--purge]        Uninstall amux and remove hooks
`)
}
