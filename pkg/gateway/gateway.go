package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"amux-accounts/pkg/identity"
)

// GatewayStatus represents the live status of the Universal AI Gateway.
type GatewayStatus struct {
	Running      bool           `json:"running"`
	PID          int            `json:"pid,omitempty"`
	Port         string         `json:"port"`
	URL          string         `json:"url"`
	ClaudeHooked bool           `json:"claude_hooked"`
	CursorHooked bool           `json:"cursor_hooked"`
	CodexHooked  bool           `json:"codex_hooked"`
	Upstream     string         `json:"upstream,omitempty"`
	Mode         string         `json:"mode,omitempty"`
	Sessions     int            `json:"sessions"`
	Pool         map[string]any `json:"pool,omitempty"`
}

// PIDFilePath returns ~/.am/gateway.pid.
func PIDFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".am", "gateway.pid")
}

// IsRunning reports whether the gateway is responding to HTTP requests.
func IsRunning() bool {
	c := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(GatewayDefaultURL + "/_am/status")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetStatus returns the current status of the gateway and hooked IDEs.
func GetStatus() GatewayStatus {
	st := GatewayStatus{
		Running: false,
		Port:    "8787",
		URL:     GatewayDefaultURL,
	}

	st.ClaudeHooked, _ = IsClaudeHooked()
	st.CursorHooked, _ = IsCursorHooked()
	st.CodexHooked, _ = IsCodexHooked()

	c := http.Client{Timeout: 1 * time.Second}
	resp, err := c.Get(GatewayDefaultURL + "/_am/status")
	if err != nil {
		return st
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		st.Running = true
		var data map[string]any
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			if up, ok := data["upstream"].(string); ok {
				st.Upstream = up
			}
			if mode, ok := data["mode"].(string); ok {
				st.Mode = mode
			}
			if sess, ok := data["sessions"].(float64); ok {
				st.Sessions = int(sess)
			}
			if p, ok := data["pool"].(map[string]any); ok {
				st.Pool = p
			}
		}
	}

	// Try reading PID
	if pidData, err := os.ReadFile(PIDFilePath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			st.PID = pid
		}
	}

	return st
}

// Start launches the detached background gateway process.
func Start() error {
	if IsRunning() {
		return fmt.Errorf("gateway is already running at %s", GatewayDefaultURL)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find self binary: %w", err)
	}

	cmd := exec.Command(self, "gateway", "_daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // detach from terminal process group
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn detached gateway: %w", err)
	}

	pid := cmd.Process.Pid
	_ = os.MkdirAll(filepath.Dir(PIDFilePath()), 0o755)
	_ = os.WriteFile(PIDFilePath(), []byte(strconv.Itoa(pid)), 0o600)

	// Wait up to 3 seconds for gateway to be ready
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("gateway process %d started but did not respond on :8787 in time", pid)
}

// Stop gracefully stops the running gateway daemon.
func Stop() error {
	if !IsRunning() {
		_ = os.Remove(PIDFilePath())
		return nil
	}

	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Post(GatewayDefaultURL+"/_am/shutdown", "application/json", nil)
	if err == nil {
		_ = resp.Body.Close()
	}

	// Wait for socket release
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning() {
			_ = os.Remove(PIDFilePath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// If PID file exists, try SIGTERM
	if pidData, err := os.ReadFile(PIDFilePath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	_ = os.Remove(PIDFilePath())
	return nil
}

// RunDaemon runs the foreground server loop intended for background daemon mode.
func RunDaemon(serverFunc func() error) error {
	pid := os.Getpid()
	_ = os.MkdirAll(filepath.Dir(PIDFilePath()), 0o755)
	_ = os.WriteFile(PIDFilePath(), []byte(strconv.Itoa(pid)), 0o600)
	defer os.Remove(PIDFilePath())

	// Start auto-hook monitor in background
	go StartAutoHookMonitor(30 * time.Second)

	return serverFunc()
}

// StartAutoHookMonitor runs a periodic check to conditionally inject or detach hooks.
func StartAutoHookMonitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		cfg, err := identity.LoadConfig("")
		if err != nil {
			continue
		}
		_ = CheckAndConditionalHook(cfg.Identities, cfg.ThresholdPct)
	}
}
