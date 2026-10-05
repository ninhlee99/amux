package tools

import (
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestParseWebTools_FuzzyBashUnescapedQuotes(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}

	// Unescaped inner quotes in shell command
	text := `<tool_call>
{"name": "Bash", "arguments": {"command": "git commit -m "fix: resolve bug""}}
</tool_call>`

	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %s", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "git commit -m") {
		t.Fatalf("expected command with git commit, got %s", calls[0].Arguments)
	}
}

func TestParseWebTools_FuzzyMissingClosingBraces(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Read", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
	}

	// Missing closing curly braces
	text := `<tool_call>
{"name": "Read", "arguments": {"file_path": "main.go"
</tool_call>`

	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected Read, got %s", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "main.go") {
		t.Fatalf("expected main.go in args, got %s", calls[0].Arguments)
	}
}

func TestParseWebTools_FuzzyFilePathAndContent(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Write", InputSchema: []byte(`{"required":["file_path","content"],"properties":{"file_path":{"type":"string"},"content":{"type":"string"}}}`)},
	}

	text := `<tool_call>
{"name": "Write", "arguments": {"file_path": "config.yaml", "content": "port: 8080"}}
</tool_call>`

	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Write" {
		t.Fatalf("expected Write, got %s", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "config.yaml") || !strings.Contains(calls[0].Arguments, "port: 8080") {
		t.Fatalf("expected file_path and content, got %s", calls[0].Arguments)
	}
}
