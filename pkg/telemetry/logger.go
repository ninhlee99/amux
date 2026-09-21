package telemetry

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu   sync.Mutex
	logOut  io.Writer = os.Stdout
	logFile *os.File
)

// InitLogging initializes logging to stdout and optional log file.
func InitLogging(filePath string) error {
	logMu.Lock()
	defer logMu.Unlock()

	if filePath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			filePath = filepath.Join(home, ".am", "gateway.log")
		}
	}

	if filePath != "" {
		_ = os.MkdirAll(filepath.Dir(filePath), 0o755)
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			logFile = f
			logOut = io.MultiWriter(os.Stdout, f)
		}
	}
	return nil
}

// LogPassthrough emits real-time structured millisecond-precision logs for passthrough events:
// [2026-09-17 15:04:05.123] [PASSTHROUGH] Claude-Client -> Claude-Upstream | Account: claude-sub-1 (82%) | Tools: 3 active | 200 OK (312ms)
func LogPassthrough(client string, upstream string, account string, toolCount int, statusCode int, duration time.Duration) {
	logMu.Lock()
	defer logMu.Unlock()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	durMs := duration.Milliseconds()

	toolInfo := ""
	if toolCount > 0 {
		toolInfo = fmt.Sprintf(" | Tools: %d active", toolCount)
	}

	acctInfo := "default"
	if account != "" {
		acctInfo = account
	}

	line := fmt.Sprintf("[%s] [PASSTHROUGH] %s -> %s | Account: %s%s | %d OK (%dms)\n",
		ts, client, upstream, acctInfo, toolInfo, statusCode, durMs)

	if logOut != nil {
		_, _ = fmt.Fprint(logOut, line)
	}
}

// LogGatewayRequest emits real-time structured log for routed gateway requests.
func LogGatewayRequest(action string, client string, upstream string, account string, toolCount int, statusCode int, duration time.Duration) {
	logMu.Lock()
	defer logMu.Unlock()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	durMs := duration.Milliseconds()

	toolInfo := ""
	if toolCount > 0 {
		toolInfo = fmt.Sprintf(" | Tools: %d active", toolCount)
	}

	line := fmt.Sprintf("[%s] [%s] %s -> %s | Account: %s%s | %d (%dms)\n",
		ts, action, client, upstream, account, toolInfo, statusCode, durMs)

	if logOut != nil {
		_, _ = fmt.Fprint(logOut, line)
	}
}
