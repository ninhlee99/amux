package cli

import (
	"fmt"
	"os"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/ui"
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
	case "status":
		CmdStatus(args)
	case "usage":
		CmdUsage(args)
	case "doctor":
		CmdDoctor(args)
	case "id":
		CmdID(args)
	case "gateway":
		CmdGateway(args)
	case "config":
		CmdConfig(args)
	case "migrate":
		CmdMigrate(args)
	case "update":
		CmdUpdate(args)
	case "uninstall":
		CmdUninstall(args)
	case "statusline":
		ui.CmdStatusline()
	default:
		die("unknown command: %s (run 'amux help' for usage)", cmd)
	}
}
