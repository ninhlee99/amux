package cli

import (
	"amux-accounts/pkg/usage"
)

// CmdUsage displays request and token analytics across day, week, and month.
func CmdUsage(args []string) {
	usage.PrintUsageReport(args)
}
