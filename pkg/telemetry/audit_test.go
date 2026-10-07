package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAuditEntry_FormatAndPermissions(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "audit.log")
	SetAuditPath(logFile)
	defer SetAuditPath("")

	entry := AuditEntry{
		Timestamp:  time.Now(),
		RequestID:  "ax_test123",
		Dialect:    "claude",
		Account:    "claude:01",
		Model:      "claude-3-7-sonnet",
		InTokens:   120,
		OutTokens:  45,
		DurationMs: 350,
		Status:     "ok",
	}

	if err := AppendAuditEntry(entry); err != nil {
		t.Fatalf("AppendAuditEntry failed: %v", err)
	}

	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	var parsed AuditEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v, raw: %s", err, string(data))
	}

	if parsed.RequestID != "ax_test123" || parsed.InTokens != 120 || parsed.OutTokens != 45 {
		t.Fatalf("audit entry mismatch: %+v", parsed)
	}
}
