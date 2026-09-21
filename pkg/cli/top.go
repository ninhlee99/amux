package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/telemetry"
)

// CmdTop displays a live dashboard of active session telemetry, CPU, memory, and worker pool metrics.
func CmdTop(args []string) {
	jsonMode := false
	onceMode := false
	interval := 1 * time.Second

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json", "-j":
			jsonMode = true
		case "--once", "-1", "-o":
			onceMode = true
		case "-n", "--interval":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil && d > 0 {
					interval = d
				} else if sec, err := strconv.Atoi(args[i+1]); err == nil && sec > 0 {
					interval = time.Duration(sec) * time.Second
				}
				i++
			}
		}
	}

	client := monitor.NewUDSClient("")

	if jsonMode {
		emitTopJSON(client)
		return
	}

	if onceMode {
		renderTopSnapshot(client)
		return
	}

	// Interactive live loop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial render
	clearScreen()
	renderTopSnapshot(client)

	for {
		select {
		case <-sigCh:
			fmt.Println()
			return
		case <-ticker.C:
			clearScreen()
			renderTopSnapshot(client)
		}
	}
}

func clearScreen() {
	// ANSI clear screen and home cursor
	fmt.Print("\033[2J\033[H")
}

func emitTopJSON(client *monitor.UDSClient) {
	if client.IsDaemonAvailable() {
		data, err := client.QueryTop()
		if err == nil {
			bytes, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(bytes))
			return
		}
	}

	// Fallback to offline store snapshot
	store := monitor.NewPTYSessionStore("")
	sessions, err := store.LoadAll()
	if err != nil {
		fmt.Printf("{\"error\": %q}\n", err.Error())
		return
	}

	var sessionList []telemetry.SessionTelemetry
	for _, s := range sessions {
		sessionList = append(sessionList, telemetry.SessionTelemetry{
			SessionID: s.SessionID,
			PID:       s.PID,
			Command:   s.Command,
			State:     string(s.State),
			CreatedAt: s.CreatedAt,
		})
	}

	offlineData := telemetry.DaemonTelemetry{
		CollectedAt:   time.Now(),
		TotalSessions: len(sessions),
		Sessions:      sessionList,
	}
	bytes, _ := json.MarshalIndent(offlineData, "", "  ")
	fmt.Println(string(bytes))
}

func renderTopSnapshot(client *monitor.UDSClient) {
	fmt.Println("=========================== AMUX SESSION TOP ===========================")

	if !client.IsDaemonAvailable() {
		fmt.Println("Daemon Status: OFFLINE (Showing snapshot from local session store)")
		fmt.Println("Tip: Start the daemon with 'amux start' for live socket metrics.")
		fmt.Println("------------------------------------------------------------------------")

		store := monitor.NewPTYSessionStore("")
		sessions, err := store.LoadAll()
		if err != nil || len(sessions) == 0 {
			fmt.Println("No recorded sessions found in local store.")
			fmt.Println("========================================================================")
			return
		}

		fmt.Printf("%-20s %-8s %-20s %-10s %-12s\n", "SESSION ID", "PID", "COMMAND", "STATE", "LAST SEEN")
		fmt.Printf("%-20s %-8s %-20s %-10s %-12s\n", "--------------------", "--------", "--------------------", "----------", "------------")

		for _, s := range sessions {
			cmdStr := truncate(s.Command, 20)
			fmt.Printf("%-20s %-8d %-20s %-10s %-12s\n",
				truncate(s.SessionID, 20), s.PID, cmdStr, s.State, time.Since(s.LastHeartbeat).Round(time.Second).String())
		}
		fmt.Println("========================================================================")
		return
	}

	data, err := client.QueryTop()
	if err != nil {
		fmt.Printf("Error fetching telemetry from daemon socket: %v\n", err)
		fmt.Println("========================================================================")
		return
	}

	fmt.Printf("Daemon: RUNNING (PID %d) | Uptime: %s | Sockets: %s\n",
		data.DaemonPID, data.DaemonUptime, monitor.DefaultSocketPath())
	fmt.Printf("Worker Pool: %d/%d Active | %d Queued | %d Completed Tasks\n",
		data.WorkerPool.ActiveWorkers, data.WorkerPool.MaxWorkers, data.WorkerPool.QueuedTasks, data.WorkerPool.CompletedTasks)
	fmt.Printf("System Load: CPU %.1f%% | Active Process Memory: %.1f MB | Sessions: %d Active / %d Total\n",
		data.TotalCPUPercent, data.TotalMemoryMB, data.ActiveSessions, data.TotalSessions)
	fmt.Println("------------------------------------------------------------------------")

	if len(data.Sessions) == 0 {
		fmt.Println("No active or recorded PTY sessions found.")
		fmt.Println("========================================================================")
		return
	}

	fmt.Printf("%-18s %-7s %-16s %-8s %-8s %-10s %-7s %-10s\n",
		"SESSION ID", "PID", "COMMAND", "STATE", "CPU %", "MEM (MB)", "TASKS", "UPTIME")
	fmt.Printf("%-18s %-7s %-16s %-8s %-8s %-10s %-7s %-10s\n",
		"------------------", "-------", "----------------", "--------", "--------", "----------", "-------", "----------")

	for _, s := range data.Sessions {
		cpuStr := fmt.Sprintf("%.1f%%", s.CPUPercent)
		memStr := fmt.Sprintf("%.1f MB", s.MemoryMB)
		tasksStr := fmt.Sprintf("%d", s.CompletedTasks)
		if s.ActiveWorkers > 0 {
			tasksStr = fmt.Sprintf("%d*", s.CompletedTasks)
		}

		fmt.Printf("%-18s %-7d %-16s %-8s %-8s %-10s %-7s %-10s\n",
			truncate(s.SessionID, 18),
			s.PID,
			truncate(s.Command, 16),
			truncate(s.State, 8),
			cpuStr,
			memStr,
			tasksStr,
			s.Uptime,
		)
	}
	fmt.Println("========================================================================")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
