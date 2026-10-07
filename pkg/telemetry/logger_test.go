package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitLogging_OwnerOnlyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.log")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitLogging(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		logMu.Lock()
		if logFile != nil {
			_ = logFile.Close()
			logFile = nil
		}
		logOut = os.Stdout
		logMu.Unlock()
	})
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("gateway.log mode = %04o, want 0600", perm)
	}
}
