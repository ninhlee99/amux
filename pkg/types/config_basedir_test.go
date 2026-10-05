package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseDir_TestBinaryNeverUsesRealHome(t *testing.T) {
	t.Setenv("AMUX_HOME", "")
	t.Setenv("AM_HOME", "")
	t.Setenv("AM_DIR", "")
	home, _ := os.UserHomeDir()
	got := BaseDir()
	if strings.HasPrefix(got, filepath.Join(home, ".amux")) || strings.HasPrefix(got, filepath.Join(home, ".am")+string(filepath.Separator)) {
		t.Fatalf("BaseDir leaked into real home under go test: %s", got)
	}
}

func TestBaseDir_EnvOverrideWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMUX_HOME", dir)
	if got := BaseDir(); got != dir {
		t.Fatalf("BaseDir = %q, want %q", got, dir)
	}
}
