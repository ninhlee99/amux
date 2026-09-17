package cli

import "fmt"

func usageHelp() {
	fmt.Print(`amux - AI Developer Infrastructure & Universal AI Gateway

Usage: amux <command> [arguments]

Core Commands:
  setup                      Guided setup wizard
  status                     Real-time dashboard of identities, quotas & gateway
  doctor                     Diagnostics for network, keychain, tools & daemon

Identity Management:
  id add [provider]          Add identity (OAuth, API key, CLI session, or CDP)
  id list                    Display flat accounts, credentials & usage %
  id remove <id>             Delete identity from persistence
  id health                  Probe token validity and remaining quotas
  id select [id]             Manual account switch / OS Keychain rotation

Gateway:
  gateway start              Start detached gateway background service
  gateway stop               Gracefully stop the daemon
  gateway status             Inspect socket status and routing tables
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
