package telemetry

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessMetrics holds CPU, Memory, and I/O metrics for a specific OS process.
type ProcessMetrics struct {
	PID         int     `json:"pid"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	MemoryMB    float64 `json:"memory_mb"`
	Threads     int     `json:"threads"`
	ReadBytes   uint64  `json:"read_bytes"`
	WriteBytes  uint64  `json:"write_bytes"`
}

var (
	cpuSamplerMu   sync.Mutex
	prevCPUSamples = make(map[int]cpuSample)
)

type cpuSample struct {
	totalTime time.Duration
	sampleAt  time.Time
}

// CollectProcessMetrics retrieves real-time CPU and Memory utilization for a given PID.
func CollectProcessMetrics(pid int) ProcessMetrics {
	if pid <= 0 {
		return ProcessMetrics{PID: pid}
	}

	metrics := ProcessMetrics{PID: pid}

	if runtime.GOOS == "linux" {
		readLinuxProcMetrics(pid, &metrics)
	} else {
		readFallbackProcMetrics(pid, &metrics)
	}

	metrics.MemoryMB = float64(metrics.MemoryBytes) / (1024 * 1024)
	return metrics
}

func readLinuxProcMetrics(pid int, m *ProcessMetrics) {
	procDir := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(procDir); err != nil {
		return
	}

	// 1. Memory from /proc/<pid>/statm (pages: total, resident, shared, text, lib, data, dt)
	statmPath := filepath.Join(procDir, "statm")
	if data, err := os.ReadFile(statmPath); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if residentPages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				pageSize := uint64(os.Getpagesize())
				m.MemoryBytes = residentPages * pageSize
			}
		}
	}

	// 2. CPU and Threads from /proc/<pid>/stat
	statPath := filepath.Join(procDir, "stat")
	if data, err := os.ReadFile(statPath); err == nil {
		statStr := string(data)
		// /proc/<pid>/stat format: pid (comm) state ppid pgrp ...
		lastParen := strings.LastIndex(statStr, ")")
		if lastParen != -1 && len(statStr) > lastParen+2 {
			rest := strings.Fields(statStr[lastParen+2:])
			// rest[0] is state (field 3)
			// rest[11] is utime (field 14)
			// rest[12] is stime (field 15)
			// rest[17] is num_threads (field 20)
			if len(rest) >= 18 {
				utime, _ := strconv.ParseUint(rest[11], 10, 64)
				stime, _ := strconv.ParseUint(rest[12], 10, 64)
				threads, _ := strconv.Atoi(rest[17])
				m.Threads = threads

				// Calculate CPU usage % over time delta
				ticks := utime + stime
				// 100 ticks per second standard on Linux
				processCPUTime := time.Duration(ticks*10) * time.Millisecond
				now := time.Now()

				cpuSamplerMu.Lock()
				prev, exists := prevCPUSamples[pid]
				prevCPUSamples[pid] = cpuSample{totalTime: processCPUTime, sampleAt: now}
				cpuSamplerMu.Unlock()

				if exists {
					timeDelta := now.Sub(prev.sampleAt).Seconds()
					if timeDelta > 0 {
						cpuDelta := (processCPUTime - prev.totalTime).Seconds()
						pct := (cpuDelta / timeDelta) * 100.0
						if pct < 0 {
							pct = 0
						}
						if pct > 800 { // Bound reasonable peak across multi-core
							pct = 800
						}
						m.CPUPercent = pct
					}
				}
			}
		}
	}

	// 3. I/O from /proc/<pid>/io (if accessible)
	ioPath := filepath.Join(procDir, "io")
	if data, err := os.ReadFile(ioPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "read_bytes:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					m.ReadBytes, _ = strconv.ParseUint(parts[1], 10, 64)
				}
			} else if strings.HasPrefix(line, "write_bytes:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					m.WriteBytes, _ = strconv.ParseUint(parts[1], 10, 64)
				}
			}
		}
	}
}

// readFallbackProcMetrics uses standard `ps` command on macOS / Darwin / BSD.
func readFallbackProcMetrics(pid int, m *ProcessMetrics) {
	cmd := exec.Command("ps", "-o", "%cpu,rss", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) >= 2 {
		fields := strings.Fields(lines[1])
		if len(fields) >= 2 {
			if cpu, err := strconv.ParseFloat(fields[0], 64); err == nil {
				m.CPUPercent = cpu
			}
			if rssKB, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				m.MemoryBytes = rssKB * 1024
			}
		}
	}
}

// SessionTelemetry combines process runtime telemetry with worker pool telemetry.
type SessionTelemetry struct {
	SessionID            string    `json:"session_id"`
	PID                  int       `json:"pid"`
	Command              string    `json:"command"`
	State                string    `json:"state"`
	CreatedAt            time.Time `json:"created_at"`
	LastHeartbeat        time.Time `json:"last_heartbeat"`
	Uptime               string    `json:"uptime"`
	CPUPercent           float64   `json:"cpu_percent"`
	MemoryMB             float64   `json:"memory_mb"`
	MemoryBytes          uint64    `json:"memory_bytes"`
	ActiveWorkers        int       `json:"active_workers"`
	CompletedTasks       uint64    `json:"completed_tasks"`
	TotalExecutionTimeMs int64     `json:"total_exec_time_ms"`
	IOReadBytes          uint64    `json:"io_read_bytes,omitempty"`
	IOWriteBytes         uint64    `json:"io_write_bytes,omitempty"`
}

// DaemonTelemetry encapsulates whole-system telemetry for `amux top`.
type DaemonTelemetry struct {
	CollectedAt     time.Time          `json:"collected_at"`
	DaemonPID       int                `json:"daemon_pid"`
	DaemonUptime    string             `json:"daemon_uptime"`
	TotalSessions   int                `json:"total_sessions"`
	ActiveSessions  int                `json:"active_sessions"`
	TotalCPUPercent float64            `json:"total_cpu_percent"`
	TotalMemoryMB   float64            `json:"total_memory_mb"`
	WorkerPool      WorkerPoolMetrics  `json:"worker_pool"`
	Sessions        []SessionTelemetry `json:"sessions"`
}

// WorkerPoolMetrics captures concurrent execution performance.
type WorkerPoolMetrics struct {
	MaxWorkers     int    `json:"max_workers"`
	ActiveWorkers  int    `json:"active_workers"`
	QueuedTasks    int    `json:"queued_tasks"`
	CompletedTasks uint64 `json:"completed_tasks"`
}
