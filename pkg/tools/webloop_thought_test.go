package tools

import (
	"testing"

	"amux-accounts/pkg/types"
)

func TestParseWebTools_WithThoughtBlock(t *testing.T) {
	input := `<thought>
I need to check the current branch and repository status before making any edits.
Let's run git status.
</thought>
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>`

	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"type":"object","properties":{"command":{"type":"string"}}}`)},
	}

	calls := ParseWebTools(input, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Errorf("expected tool Bash, got %s", calls[0].Name)
	}
	if calls[0].Arguments != `{"command":"git status"}` {
		t.Errorf("unexpected arguments: %s", calls[0].Arguments)
	}

	stripped := StripWebToolMarkup(input)
	if stripped != "" {
		t.Errorf("expected empty stripped text after thought + tool call, got %q", stripped)
	}
}
