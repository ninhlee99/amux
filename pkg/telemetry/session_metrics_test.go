package telemetry

import (
	"os"
	"testing"
)

func TestCollectProcessMetrics_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	m := CollectProcessMetrics(pid)

	if m.PID != pid {
		t.Errorf("expected PID %d, got %d", pid, m.PID)
	}
	if m.MemoryBytes == 0 {
		t.Logf("Notice: memory bytes returned 0 (platform specific or test environment)")
	}
	if m.MemoryMB < 0 {
		t.Errorf("expected non-negative MemoryMB, got %f", m.MemoryMB)
	}
	if m.CPUPercent < 0 {
		t.Errorf("expected non-negative CPUPercent, got %f", m.CPUPercent)
	}
}

func TestCollectProcessMetrics_InvalidPID(t *testing.T) {
	m := CollectProcessMetrics(-1)
	if m.PID != -1 {
		t.Errorf("expected PID -1, got %d", m.PID)
	}
	if m.MemoryBytes != 0 {
		t.Errorf("expected 0 memory for invalid PID, got %d", m.MemoryBytes)
	}
}
