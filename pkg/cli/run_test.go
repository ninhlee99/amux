package cli

import (
	"testing"
)

func TestCmdRun_HelpNoArgs(t *testing.T) {
	// Calling with no args should print usage and return safely without exit
	CmdRun([]string{})
}
