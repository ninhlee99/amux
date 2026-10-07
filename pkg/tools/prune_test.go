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

func TestPruneToolResult_150LinesCodePreserved(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 150; i++ {
		sb.WriteString(fmt.Sprintf("const value%d = %d;\n", i, i))
	}
	raw := sb.String()
	got := PruneToolResult(raw)
	if got != strings.TrimSpace(raw) {
		t.Fatalf("150-line code file under 24KB must NOT be truncated, got length %d vs %d", len(got), len(raw))
	}
}

func TestPruneToolResult_MultiLinePruning(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 500; i++ {
		sb.WriteString(fmt.Sprintf("line %03d: log message for step\n", i))
	}
	raw := sb.String()
	pruned := PruneToolResult(raw)

	if !strings.Contains(pruned, "line 001: log message") {
		t.Fatalf("pruned output should contain head line 1")
	}
	if !strings.Contains(pruned, "line 100: log message") {
		t.Fatalf("pruned output should contain head line 100")
	}
	if !strings.Contains(pruned, "line 500: log message") {
		t.Fatalf("pruned output should contain tail line 500")
	}
	if !strings.Contains(pruned, "[... truncated 280 lines of tool output ...]") {
		t.Fatalf("pruned output should contain truncation indicator, got:\n%s", pruned)
	}
}

func TestPruneToolResult_LongSingleLinePayload(t *testing.T) {
	longLine := strings.Repeat("A", 30000)
	pruned := PruneToolResult(longLine)

	if len(pruned) >= 30000 {
		t.Fatalf("expected pruned length < 30000, got %d", len(pruned))
	}
	if !strings.Contains(pruned, "[... truncated verbose payload ...]") {
		t.Fatalf("expected payload truncation marker, got:\n%s", pruned)
	}
}
