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
	}
}
