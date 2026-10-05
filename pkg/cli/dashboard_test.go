package cli

import (
	"testing"
)

func TestCmdDashboard_OnceAndJSON(t *testing.T) {
	// Verify once mode does not panic
	CmdDashboard([]string{"--once"})
	// Verify json mode does not panic
	CmdDashboard([]string{"--json"})
}
