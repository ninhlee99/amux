package cli

import (
	"strings"
	"testing"
)

func TestCmdCompletion(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, sh := range shells {
		out := captureStdout(func() {
			CmdCompletion([]string{sh})
		})
		if len(out) == 0 {
			t.Errorf("expected non-empty output for shell %s", sh)
		}
		if !strings.Contains(out, "amux") {
			t.Errorf("expected completion script for %s to reference 'amux'", sh)
		}
		if !strings.Contains(out, "run") || !strings.Contains(out, "dashboard") {
			t.Errorf("expected completion script for %s to contain 'run' and 'dashboard'", sh)
		}
		if !strings.Contains(out, "windsurf") {
			t.Errorf("expected completion script for %s to contain 'windsurf'", sh)
		}
	}
}
