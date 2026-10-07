package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"amux-accounts/pkg/types"
)

// AuditEntry captures privacy-safe metadata for an executed gateway turn.
// Raw prompts, user input, and completion texts are never included.
type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	RequestID  string    `json:"request_id"`
	Dialect    string    `json:"dialect"`
	Account    string    `json:"account"`
	Model      string    `json:"model"`
	InTokens   int       `json:"tokens_in"`
	OutTokens  int       `json:"tokens_out"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // "ok" | "err"
	Error      string    `json:"error,omitempty"`
}

var (
	auditMu   sync.Mutex
	auditPath string
)

// SetAuditPath overrides the default audit log location (used for tests).
func SetAuditPath(p string) {
	auditMu.Lock()
	defer auditMu.Unlock()
	auditPath = p
}

func defaultAuditPath() string {
	if auditPath != "" {
		return auditPath
	}
	return filepath.Join(types.BaseDir(), "audit.log")
}

// AppendAuditEntry writes an audit entry in JSONL format with 0600 permissions.
func AppendAuditEntry(e AuditEntry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	auditMu.Lock()
	defer auditMu.Unlock()

	p := defaultAuditPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_ = f.Chmod(0o600)
	_, err = f.Write(data)
	return err
}
