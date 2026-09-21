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

	if hasHelp(args) {
		switch cmd {
		case "start":
			helpStart()
			return
		case "stop":
			helpStop()
			return
		case "restart":
			helpRestart()
			return
		case "status":
			helpStatus()
			return
		case "login":
			helpLogin()
			return
		case "switch":
			helpSwitch()
			return
		case "account", "accounts", "id":
			helpAccount()
			return
		case "hook":
			helpHook()
			return
		case "unhook":
			helpUnhook()
			return
		case "doctor":
			helpDoctor()
			return
		case "usage":
			helpUsage()
			return
		case "env":
			helpEnv()
			return
		default:
			usageHelp()
			return
		}
	}

	switch cmd {
	case "help", "-h", "--help":
		usageHelp()
	case "login":
		ui.CmdLogin(args)
	case "start":
		cmdGatewayStart(args)
	case "stop":
		cmdGatewayStop(args)
	case "restart":
		cmdGatewayRestart(args)
	case "status":
		CmdStatus(args)
	case "account", "accounts", "id":
		CmdAccount(args)
	case "switch":
		cmdIDSelect(args)
	case "hook":
		cmdGatewayHook(args)
	case "unhook":
		cmdGatewayUnhook(args)
	case "env":
		CmdEnv(args)
	case "gateway":
		CmdGateway(args)
	case "usage":
		CmdUsage(args)
	case "doctor":
		CmdDoctor(args)
	case "audit":
		CmdAudit(args)
	case "setup":
		CmdSetup(args)
	case "config":
		CmdConfig(args)
	case "threshold":
		CmdConfig(append([]string{"threshold"}, args...))
	case "migrate":
		CmdMigrate(args)
	case "update":
		CmdUpdate(args)
	case "uninstall":
		CmdUninstall(args)
	case "feedback":
		CmdFeedback(args)
	case "statusline":
		ui.CmdStatusline()
	default:
		die("unknown command: %s (run 'amux help' for usage)", cmd)
	}
}
