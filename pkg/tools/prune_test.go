package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestPruneToolResult_ShortOutputUnchanged(t *testing.T) {
	short := "total 12\n-rw-r--r-- 1 root root 1234 main.go\n"
	got := PruneToolResult(short)
	if got != strings.TrimSpace(short) {
		t.Fatalf("expected exact match, got %q", got)
	}
}

func TestPruneToolResult_MultiLinePruning(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString(fmt.Sprintf("line %d: log message for step\n", i))
	}
	raw := sb.String()
	pruned := PruneToolResult(raw)

	if !strings.Contains(pruned, "line 1: log message") {
		t.Fatalf("pruned output should contain head line 1")
	}
	if !strings.Contains(pruned, "line 30: log message") {
		t.Fatalf("pruned output should contain head line 30")
	}
	if !strings.Contains(pruned, "line 200: log message") {
		t.Fatalf("pruned output should contain tail line 200")
	}
	if !strings.Contains(pruned, "[... truncated 130 lines of tool output ...]") {
		t.Fatalf("pruned output should contain truncation indicator, got:\n%s", pruned)
	}
}

func TestPruneToolResult_LongSingleLinePayload(t *testing.T) {
	longLine := strings.Repeat("A", 6000)
	pruned := PruneToolResult(longLine)

	if len(pruned) >= 6000 {
		t.Fatalf("expected pruned length < 6000, got %d", len(pruned))
	}
	if !strings.Contains(pruned, "[... truncated verbose payload ...]") {
		t.Fatalf("expected payload truncation marker, got:\n%s", pruned)
	}
}
