package monitor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/telemetry"
	"amux-accounts/pkg/types"
)

// DefaultSocketPath returns the standard Unix Domain Socket file path (~/.amux/amux.sock).
func DefaultSocketPath() string {
	return filepath.Join(types.BaseDir(), "amux.sock")
}

// UDSRequest defines the schema for IPC messages sent from CLI commands to the daemon.
type UDSRequest struct {
	Action    string          `json:"action"` // "top", "list", "inspect", "kill", "reconnect", "ping"
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// UDSResponse defines the response schema returned by the daemon.
type UDSResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// UDSServer provides a Unix Domain Socket IPC interface for external CLI commands.
type UDSServer struct {
	socketPath string
	monitor    *PTYHeartbeatMonitor
	pool       *guard.WorkerPool
	listener   net.Listener
	closed     atomic.Bool
	stopCh     chan struct{}
	mu         sync.Mutex
	startTime  time.Time
}

var (
	globalUDSServer     *UDSServer
	globalUDSServerOnce sync.Once
)

// GlobalUDSServer returns the global UDS server instance.
func GlobalUDSServer() *UDSServer {
	globalUDSServerOnce.Do(func() {
		globalUDSServer = NewUDSServer("", GlobalPTYMonitor(), guard.GlobalWorkerPool())
	})
	return globalUDSServer
}

// NewUDSServer creates a new UDS IPC server instance.
func NewUDSServer(socketPath string, mon *PTYHeartbeatMonitor, pool *guard.WorkerPool) *UDSServer {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	if mon == nil {
		mon = GlobalPTYMonitor()
	}
	if pool == nil {
		pool = guard.GlobalWorkerPool()
	}

	return &UDSServer{
		socketPath: socketPath,
		monitor:    mon,
		pool:       pool,
		stopCh:     make(chan struct{}),
		startTime:  time.Now(),
	}
}

// Start begins listening on the Unix Domain Socket in a background goroutine.
func (s *UDSServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return fmt.Errorf("uds server is closed")
	}
	if s.listener != nil {
		return nil // already running
	}

	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	// Remove existing stale socket if present
	_ = os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket %s: %w", s.socketPath, err)
	}
	_ = os.Chmod(s.socketPath, 0o600)

	s.listener = ln
	s.startTime = time.Now()

	go s.acceptLoop()
	return nil
}

func (s *UDSServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			select {
			case <-s.stopCh:
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *UDSServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return
	}
	if len(line) == 0 {
		return
	}

	var req UDSRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(conn, "malformed request: "+err.Error())
		return
	}

	resp := s.dispatch(req)
	respBytes, err := json.Marshal(resp)
	if err != nil {
		s.writeError(conn, "failed to marshal response: "+err.Error())
		return
	}

	_, _ = conn.Write(append(respBytes, '\n'))
}

func (s *UDSServer) dispatch(req UDSRequest) UDSResponse {
	switch req.Action {
	case "ping":
		return s.handlePing()
	case "top", "telemetry":
		return s.handleTop()
	case "list", "sessions":
		return s.handleList()
	case "inspect":
		return s.handleInspect(req.SessionID)
	case "kill":
		return s.handleKill(req.SessionID)
	case "reconnect":
		return s.handleReconnect(req.SessionID)
	default:
		return UDSResponse{Success: false, Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

func (s *UDSServer) handlePing() UDSResponse {
	data, _ := json.Marshal(map[string]any{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
		"pid":    os.Getpid(),
	})
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) handleTop() UDSResponse {
	sessions := s.monitor.ListSessions()
	workerStats := s.pool.GetAllSessionStats()

	var sessionTelemetryList []telemetry.SessionTelemetry
	var totalCPU float64
	var totalMemMB float64
	activeCount := 0

	for _, sess := range sessions {
		procMetrics := telemetry.CollectProcessMetrics(sess.PID)
		wStat := workerStats[sess.SessionID]

		uptime := time.Since(sess.CreatedAt).Round(time.Second).String()
		if sess.CreatedAt.IsZero() {
			uptime = "-"
		}

		if sess.State == PTYStateActive {
			activeCount++
			totalCPU += procMetrics.CPUPercent
			totalMemMB += procMetrics.MemoryMB
		}

		sessionTelemetryList = append(sessionTelemetryList, telemetry.SessionTelemetry{
			SessionID:            sess.SessionID,
			PID:                  sess.PID,
			Command:              sess.Command,
			State:                string(sess.State),
			CreatedAt:            sess.CreatedAt,
			LastHeartbeat:        sess.LastHeartbeat,
			Uptime:               uptime,
			CPUPercent:           procMetrics.CPUPercent,
			MemoryMB:             procMetrics.MemoryMB,
			MemoryBytes:          procMetrics.MemoryBytes,
			ActiveWorkers:        wStat.ActiveWorkers,
			CompletedTasks:       wStat.CompletedTasks,
			TotalExecutionTimeMs: wStat.TotalExecutionTimeMs,
			IOReadBytes:          procMetrics.ReadBytes,
			IOWriteBytes:         procMetrics.WriteBytes,
		})
	}

	daemonTele := telemetry.DaemonTelemetry{
		CollectedAt:     time.Now(),
		DaemonPID:       os.Getpid(),
		DaemonUptime:    time.Since(s.startTime).Round(time.Second).String(),
		TotalSessions:   len(sessions),
		ActiveSessions:  activeCount,
		TotalCPUPercent: totalCPU,
		TotalMemoryMB:   totalMemMB,
		WorkerPool: telemetry.WorkerPoolMetrics{
			MaxWorkers:     s.pool.MaxWorkers(),
			ActiveWorkers:  s.pool.ActiveWorkers(),
			QueuedTasks:    s.pool.QueuedTasks(),
			CompletedTasks: s.pool.CompletedTasks(),
		},
		Sessions: sessionTelemetryList,
	}

	data, err := json.Marshal(daemonTele)
	if err != nil {
		return UDSResponse{Success: false, Error: "marshal top telemetry: " + err.Error()}
	}
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) handleList() UDSResponse {
	sessions := s.monitor.ListSessions()
	data, _ := json.Marshal(sessions)
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) handleInspect(sessionID string) UDSResponse {
	if sessionID == "" {
		return UDSResponse{Success: false, Error: "missing session_id"}
	}
	sess, ok := s.monitor.GetSession(sessionID)
	if !ok {
		return UDSResponse{Success: false, Error: fmt.Sprintf("session %q not found", sessionID)}
	}
	data, _ := json.Marshal(sess)
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) handleKill(sessionID string) UDSResponse {
	if sessionID == "" {
		return UDSResponse{Success: false, Error: "missing session_id"}
	}
	sess, ok := s.monitor.GetSession(sessionID)
	if !ok {
		return UDSResponse{Success: false, Error: fmt.Sprintf("session %q not found", sessionID)}
	}

	if sess.PID > 0 {
		proc, err := os.FindProcess(sess.PID)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
	s.monitor.Unregister(sessionID)

	data, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"status":     "killed",
	})
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) handleReconnect(sessionID string) UDSResponse {
	if sessionID == "" {
		return UDSResponse{Success: false, Error: "missing session_id"}
	}
	sess, err := s.monitor.Reconnect(sessionID)
	if err != nil {
		return UDSResponse{Success: false, Error: err.Error()}
	}
	data, _ := json.Marshal(sess)
	return UDSResponse{Success: true, Data: data}
}

func (s *UDSServer) writeError(conn net.Conn, msg string) {
	resp := UDSResponse{Success: false, Error: msg}
	bytes, _ := json.Marshal(resp)
	_, _ = conn.Write(append(bytes, '\n'))
}

// Stop cleanly closes the UDS listener and removes the socket file.
func (s *UDSServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.CompareAndSwap(false, true) {
		close(s.stopCh)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		_ = os.Remove(s.socketPath)
	}
	return nil
}

// =========================================================================
// UDS Client for external CLI commands
// =========================================================================

// UDSClient connects to the running AMUX daemon over Unix Domain Socket.
type UDSClient struct {
	socketPath string
	timeout    time.Duration
}

// NewUDSClient initializes a client targeting the daemon's UDS.
func NewUDSClient(socketPath string) *UDSClient {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &UDSClient{
		socketPath: socketPath,
		timeout:    2 * time.Second,
	}
}

// Call sends a request to the daemon over UDS and parses the response.
func (c *UDSClient) Call(req UDSRequest) (*UDSResponse, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("dial daemon socket %s: %w", c.socketPath, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(c.timeout))

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if _, err := conn.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp UDSResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !resp.Success {
		return &resp, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// IsDaemonAvailable checks if the daemon's UDS socket is actively listening.
func (c *UDSClient) IsDaemonAvailable() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// QueryTop fetches comprehensive real-time telemetry from the daemon.
func (c *UDSClient) QueryTop() (*telemetry.DaemonTelemetry, error) {
	resp, err := c.Call(UDSRequest{Action: "top"})
	if err != nil {
		return nil, err
	}

	var dt telemetry.DaemonTelemetry
	if err := json.Unmarshal(resp.Data, &dt); err != nil {
		return nil, fmt.Errorf("unmarshal top telemetry: %w", err)
	}
	return &dt, nil
}

// QuerySessions fetches the list of active/tracked sessions.
func (c *UDSClient) QuerySessions() ([]PTYSession, error) {
	resp, err := c.Call(UDSRequest{Action: "list"})
	if err != nil {
		return nil, err
	}

	var list []PTYSession
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, fmt.Errorf("unmarshal sessions: %w", err)
	}
	return list, nil
}

// KillSession asks the daemon to terminate and unregister a session.
func (c *UDSClient) KillSession(sessionID string) error {
	_, err := c.Call(UDSRequest{Action: "kill", SessionID: sessionID})
	return err
}

// ReconnectSession asks the daemon for session connection details.
func (c *UDSClient) ReconnectSession(sessionID string) (*PTYSession, error) {
	resp, err := c.Call(UDSRequest{Action: "reconnect", SessionID: sessionID})
	if err != nil {
		return nil, err
	}

	var sess PTYSession
	if err := json.Unmarshal(resp.Data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal reconnect: %w", err)
	}
	return &sess, nil
}
