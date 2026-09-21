package cli

import (
	"testing"
)

func TestCmdTop_Flags(t *testing.T) {
	// Test --json flag execution (runs and completes without panic)
	CmdTop([]string{"--json"})

	// Test --once flag execution (runs snapshot and exits)
	CmdTop([]string{"--once"})
}
