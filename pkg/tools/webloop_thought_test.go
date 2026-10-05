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

func TestWrapWebStream_StreamsThinkingImmediately(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"type":"object","properties":{"command":{"type":"string"}}}`)},
	}

	inner := make(chan types.StreamChunk, 2)
	inner <- types.StreamChunk{ID: "chunk1", Thinking: "Deep reasoning about the task..."}
	inner <- types.StreamChunk{ID: "chunk2", Content: "<tool_call>\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"git status\"}}\n</tool_call>", Done: true}
	close(inner)

	req := &types.ChatRequest{Tools: defs}
	out := MaybeWrapWebStream("claude:web:01", req, inner)

	var receivedThinking string
	var receivedCalls []types.ToolCall

	for c := range out {
		if c.Thinking != "" {
			receivedThinking += c.Thinking
		}
		if len(c.ToolCalls) > 0 {
			receivedCalls = append(receivedCalls, c.ToolCalls...)
		}
	}

	if receivedThinking != "Deep reasoning about the task..." {
		t.Fatalf("expected streamed thinking, got %q", receivedThinking)
	}
	if len(receivedCalls) != 1 || receivedCalls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash tool call, got %+v", receivedCalls)
	}
}
