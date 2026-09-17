package cli

import (
	"fmt"
	"os"

	"amux-accounts/pkg/monitor"
)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "amux: "+format+"\n", a...)
	os.Exit(1)
}

// Run executes the amux CLI command with the given argument list.
func Run(rawArgs []string) {
	if len(rawArgs) < 2 {
		usageHelp()
		return
	}
	monitor.EnableTermSink()
	autoMigrateCheck()

	cmd := rawArgs[1]
	args := rawArgs[2:]

	switch cmd {
	case "help", "-h", "--help":
		usageHelp()
	case "setup":
		CmdSetup(args)
	case "status", "st":
		CmdStatus(args)
	case "doctor":
		CmdDoctor(args)
	case "id", "identity":
		CmdID(args)
	case "gateway", "gw", "proxy":
		CmdGateway(args)
	case "config", "cfg":
		CmdConfig(args)
	case "migrate":
		CmdMigrate(args)
	case "update", "upgrade":
		CmdUpdate(args)
	case "uninstall":
		CmdUninstall(args)
	case "ls", "accounts":
		CmdID(append([]string{"list"}, args...))
	case "add":
		CmdID(append([]string{"add"}, args...))
	case "rm", "remove", "delete":
		CmdID(append([]string{"remove"}, args...))
	case "sw", "switch":
		CmdID(append([]string{"select"}, args...))
	case "run":
		die("'amux run' is removed. Run your IDE natively (e.g. 'claude', 'codex', 'cursor'). AMUX operates non-invasively via OS Keychain rotation.")
	default:
		die("unknown command: %s (run 'amux help' for usage)", cmd)
	}
}
