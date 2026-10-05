package tools

import (
	"fmt"
	"strings"
)

const (
	// DefaultMaxToolOutputBytes is the threshold before pruning large tool outputs for web prompts.
	DefaultMaxToolOutputBytes = 4000
	// DefaultMaxToolOutputLines is the line threshold before pruning large tool outputs.
	DefaultMaxToolOutputLines = 80
	// DefaultPruneHeadLines is the number of leading lines preserved.
	DefaultPruneHeadLines = 30
	// DefaultPruneTailLines is the number of trailing lines preserved.
	DefaultPruneTailLines = 40
)

// PruneToolResult compacts overly long CLI/tool outputs so they fit comfortably
// inside Web chat context limits while preserving initial commands and final errors/exit codes.
func PruneToolResult(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= DefaultMaxToolOutputBytes {
		lines := strings.Split(s, "\n")
		if len(lines) <= DefaultMaxToolOutputLines {
			return s
		}
	}

	lines := strings.Split(s, "\n")
	if len(lines) > (DefaultPruneHeadLines + DefaultPruneTailLines) {
		head := lines[:DefaultPruneHeadLines]
		tail := lines[len(lines)-DefaultPruneTailLines:]
		omitted := len(lines) - DefaultPruneHeadLines - DefaultPruneTailLines
		return fmt.Sprintf("%s\n\n[... truncated %d lines of tool output ...]\n\n%s",
			strings.Join(head, "\n"),
			omitted,
			strings.Join(tail, "\n"),
		)
	}

	// Long lines but fewer total lines (e.g. minified single-line payload)
	if len(s) > DefaultMaxToolOutputBytes {
		runes := []rune(s)
		headCount := 1800
		tailCount := 1800
		if len(runes) > headCount+tailCount {
			return fmt.Sprintf("%s\n\n[... truncated verbose payload ...]\n\n%s",
				string(runes[:headCount]),
				string(runes[len(runes)-tailCount:]),
			)
		}
	}

	return s
}
