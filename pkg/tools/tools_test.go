package tools

import (
	"encoding/json"
	"testing"

	"amux-accounts/pkg/types"
)

func TestClaudeCursorRoundTrip(t *testing.T) {
	body := []byte(`{
		"tools":[{
			"name":"Bash",
			"description":"Run a shell command",
			"input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}
		}]
	}`)
	defs, err := ParseClaudeTools(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "Bash" {
		t.Fatalf("defs=%+v", defs)
	}

	oa := ToCursorTools(defs)
	if len(oa) != 1 || oa[0].Type != "function" || oa[0].Function.Name != "Bash" {
		t.Fatalf("cursor=%+v", oa)
	}
	raw, _ := json.Marshal(map[string]any{"tools": oa})
	back, err := ParseCursorTools(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Name != "Bash" {
		t.Fatalf("back=%+v", back)
	}

	// Codex shares the OpenAI wire — same defs round-trip.
	codexBack, err := ParseCodexTools(raw)
	if err != nil || len(codexBack) != 1 || codexBack[0].Name != "Bash" {
		t.Fatalf("codex back=%+v err=%v", codexBack, err)
	}
}

func TestGeminiRoundTrip(t *testing.T) {
	defs := []types.ToolDef{{
		Name:        "Read",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}}
	fns := ToGeminiFunctions(defs)
	back := FromGeminiFunctions(fns)
	if len(back) != 1 || back[0].Name != "Read" {
		t.Fatalf("back=%+v", back)
	}

	calls := []types.ToolCall{{ID: "x", Name: "Read", Arguments: `{"path":"a.go"}`}}
	gCalls := ToGeminiFunctionCalls(calls)
	from := FromGeminiFunctionCalls(gCalls)
	if len(from) != 1 || from[0].Name != "Read" || from[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("from=%+v", from)
	}
}

func TestToolCallClaudeCursor(t *testing.T) {
	calls := []types.ToolCall{{ID: "toolu_1", Name: "Bash", Arguments: `{"command":"ls"}`}}
	blocks := ToClaudeToolUseBlocks(calls)
	if len(blocks) != 1 || blocks[0].Type != "tool_use" || blocks[0].Name != "Bash" {
		t.Fatalf("blocks=%+v", blocks)
	}
	oa := ToCursorToolCalls(calls)
	back := FromCursorToolCalls(oa)
	if len(back) != 1 || back[0].ID != "toolu_1" || back[0].Arguments != `{"command":"ls"}` {
		t.Fatalf("back=%+v", back)
	}
}

func TestMarshalClaudeMessagesRequest(t *testing.T) {
	req := &types.ChatRequest{
		Model: "claude-3-7-sonnet-20250219",
		Tools: []types.ToolDef{
			{
				Name:        "exec_command",
				Description: "Run a shell command",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
			},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are a helpful coding assistant."},
			{Role: "user", Content: "List directory contents."},
			{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{ID: "call_1", Name: "exec_command", Arguments: `{"cmd":"ls -la"}`},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "file1.txt\nfile2.txt"},
		},
	}

	b, err := MarshalClaudeMessagesRequest(req, "")
	if err != nil {
		t.Fatalf("MarshalClaudeMessagesRequest failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if m["system"] != "You are a helpful coding assistant." {
		t.Errorf("unexpected system: %v", m["system"])
	}

	toolsList, ok := m["tools"].([]any)
	if !ok || len(toolsList) != 1 {
		t.Fatalf("unexpected tools: %v", m["tools"])
	}

	msgs, ok := m["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected 3 coalesced messages (user, assistant, user), got %d: %+v", len(msgs), msgs)
	}

	// First: user
	m0 := msgs[0].(map[string]any)
	if m0["role"] != "user" {
		t.Errorf("msg 0 role expected user, got %v", m0["role"])
	}

	// Second: assistant with tool_use
	m1 := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Errorf("msg 1 role expected assistant, got %v", m1["role"])
	}
	content1 := m1["content"].([]any)
	if len(content1) != 1 || content1[0].(map[string]any)["type"] != "tool_use" {
		t.Errorf("msg 1 content expected tool_use, got %+v", content1)
	}

	// Third: user with tool_result
	m2 := msgs[2].(map[string]any)
	if m2["role"] != "user" {
		t.Errorf("msg 2 role expected user, got %v", m2["role"])
	}
	content2 := m2["content"].([]any)
	if len(content2) != 1 || content2[0].(map[string]any)["type"] != "tool_result" {
		t.Errorf("msg 2 content expected tool_result, got %+v", content2)
	}
}

