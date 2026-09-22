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
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
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
	AgyHooked    bool           `json:"agy_hooked"`
	Upstream     string         `json:"upstream,omitempty"`
	Mode         string         `json:"mode,omitempty"`
	Sessions     int            `json:"sessions"`
	PublicMode   bool           `json:"public_mode"`
	Pool         map[string]any `json:"pool,omitempty"`
}

// PIDFilePath returns ~/.amux/gateway.pid.
func PIDFilePath() string {
	return filepath.Join(types.BaseDir(), "gateway.pid")
}

// IsRunning reports whether the gateway is responding to HTTP requests.
func IsRunning() bool {
	return probeStatus() == probeUp
}

type probeResult int

const (
	// probeDown means nothing answered (connection refused/timeout) —
	// the gateway may just not have bound its listener yet.
	probeDown probeResult = iota
	// probeUp means amux's own gateway answered.
	probeUp
	// probeOccupied means something answered on the port, but it isn't
	// amux — some unrelated local process is squatting on :8787 (this
	// has happened in practice and made `amux start` spin for its whole
	// timeout waiting on a health check that was talking to the wrong
	// server).
	probeOccupied
)

// probeStatus checks /_am/status once and classifies the result. It relies
// on the marker header amux's own handler always sets — a bare HTTP 200
// isn't proof it's *our* gateway.
func probeStatus() probeResult {
	c := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(GatewayDefaultURL + "/_am/status")
	if err != nil {
		return probeDown
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && resp.Header.Get(proxy.AmuxGatewayHeader) != "" {
		return probeUp
	}
	return probeOccupied
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
	st.AgyHooked, _ = IsAgyHooked()

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

	// Wait up to 8 seconds for gateway to be ready. Startup does some
	// synchronous local I/O (account/profile loading) before it can bind
	// the listener, so a few seconds of slack avoids false-negative
	// timeouts on a slower machine even though the daemon is healthy.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		switch probeStatus() {
		case probeUp:
			return nil
		case probeOccupied:
			// Some other process is answering on :8787 right now — that's
			// not going to change while we keep polling, so fail fast
			// instead of burning the rest of the deadline.
			return fmt.Errorf("gateway process %d started, but port 8787 is already in use by a different process (not amux) — stop whatever else is listening there, or check 'lsof -i :8787', and try again", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("gateway process %d started but did not respond on :8787 in time (it may still come up in the background — check with 'amux status')", pid)
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
