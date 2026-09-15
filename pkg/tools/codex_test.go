package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestParseCodexResponsesTools_FlatAndNested(t *testing.T) {
	flat := []byte(`[
		{"type":"function","name":"Bash","description":"shell","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}
	]`)
	defs := ParseCodexResponsesTools(flat)
	if len(defs) != 1 || defs[0].Name != "Bash" {
		t.Fatalf("flat=%+v", defs)
	}
	if !strings.Contains(string(defs[0].InputSchema), "command") {
		t.Fatalf("schema=%s", defs[0].InputSchema)
	}

	nested := []byte(`[
		{"type":"function","function":{"name":"Read","description":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}
	]`)
	nestedDefs := ParseCodexResponsesTools(nested)
	if len(nestedDefs) != 1 || nestedDefs[0].Name != "Read" {
		t.Fatalf("nested=%+v", nestedDefs)
	}
}

func TestMarshalCodexResponsesRequest_NativeToolsAndResults(t *testing.T) {
	req := &types.ChatRequest{
		Model:  "gpt-5.6-terra",
		Stream: true,
		Tools: []types.ToolDef{
			{
				Name:        "Bash",
				Description: "Run shell",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
			},
		},
		Messages: []types.ChatMessage{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "list files"},
			{Role: "assistant", ToolCalls: []types.ToolCall{
				{ID: "toolu_1", Name: "Bash", Arguments: `{"command":"ls"}`},
			}},
			{Role: "tool", ToolCallID: "toolu_1", Name: "Bash", Content: "main.go\n"},
		},
	}
	raw, err := MarshalCodexResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Model             string               `json:"model"`
		Stream            bool                 `json:"stream"`
		Store             bool                 `json:"store"`
		ParallelToolCalls bool                 `json:"parallel_tool_calls"`
		Instructions      string               `json:"instructions"`
		Tools             []CodexResponsesTool `json:"tools"`
		Input             []map[string]any     `json:"input"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "gpt-5.6-terra" || !payload.Stream || payload.Store {
		t.Fatalf("envelope=%+v", payload)
	}
	if !payload.ParallelToolCalls || len(payload.Tools) != 1 || payload.Tools[0].Name != "Bash" {
		t.Fatalf("tools=%+v", payload.Tools)
	}
	if payload.Tools[0].Type != "function" {
		t.Fatalf("tool type=%s", payload.Tools[0].Type)
	}
	if !strings.Contains(string(payload.Tools[0].Parameters), "command") {
		t.Fatalf("parameters=%s", payload.Tools[0].Parameters)
	}
	if payload.Instructions != "Be concise." {
		t.Fatalf("instructions=%q", payload.Instructions)
	}
	if len(payload.Input) != 3 {
		t.Fatalf("input=%+v", payload.Input)
	}
	if payload.Input[0]["role"] != "user" {
		t.Fatalf("user=%+v", payload.Input[0])
	}
	if payload.Input[1]["type"] != "function_call" || payload.Input[1]["call_id"] != "toolu_1" {
		t.Fatalf("function_call=%+v", payload.Input[1])
	}
	if payload.Input[2]["type"] != "function_call_output" || payload.Input[2]["call_id"] != "toolu_1" {
		t.Fatalf("function_call_output=%+v", payload.Input[2])
	}
}

func TestProxyTools_ClaudeCodexAGYRoundTrip(t *testing.T) {
	// Claude Code catalog → Codex Responses + AGY Gemini → back to Claude.
	claudeBody := []byte(`{"tools":[
		{"name":"Bash","description":"shell","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}},
		{"name":"Read","description":"read","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
	]}`)
	defs, err := ParseClaudeTools(claudeBody)
	if err != nil || len(defs) != 2 {
		t.Fatalf("parse claude: %v %+v", err, defs)
	}

	codexTools := ToCodexResponsesTools(defs)
	codexRaw, _ := json.Marshal(codexTools)
	codexDefs := ParseCodexResponsesTools(codexRaw)
	if len(codexDefs) != 2 || codexDefs[0].Name != "Bash" || codexDefs[1].Name != "Read" {
		t.Fatalf("codex responses=%+v", codexDefs)
	}

	agyDefs := FromGeminiFunctions(ToGeminiFunctions(defs))
	if len(agyDefs) != 2 || agyDefs[1].Name != "Read" {
		t.Fatalf("agy=%+v", agyDefs)
	}

	backClaude := ToClaudeTools(codexDefs)
	if len(backClaude) != 2 || backClaude[0].Name != "Bash" {
		t.Fatalf("back claude=%+v", backClaude)
	}
	if !strings.Contains(string(backClaude[0].InputSchema), "command") {
		t.Fatalf("claude schema lost: %s", backClaude[0].InputSchema)
	}

	calls := []types.ToolCall{{ID: "toolu_x", Name: "Bash", Arguments: `{"command":"pwd"}`}}
	viaCodex := FromCodexToolCalls(ToCodexToolCalls(calls))
	viaAGY := FromGeminiFunctionCalls(ToGeminiFunctionCalls(calls))
	viaClaude := ToClaudeToolUseBlocks(viaCodex)
	if viaClaude[0].ID != "toolu_x" || string(viaClaude[0].Input) != `{"command":"pwd"}` {
		t.Fatalf("claude blocks=%+v", viaClaude)
	}
	if viaAGY[0].Name != "Bash" || viaAGY[0].Arguments != `{"command":"pwd"}` {
		t.Fatalf("agy calls=%+v", viaAGY)
	}

	// Codex client catalog (flat Responses) → Claude + AGY.
	codexClient := ParseCodexResponsesTools([]byte(`[
		{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}
	]`))
	if ToClaudeTools(codexClient)[0].Name != "exec_command" {
		t.Fatalf("codex→claude name=%s", ToClaudeTools(codexClient)[0].Name)
	}
	if ToGeminiFunctions(codexClient)[0].Name != "exec_command" {
		t.Fatalf("codex→agy name=%s", ToGeminiFunctions(codexClient)[0].Name)
	}

	// AGY client catalog → Claude + Codex Responses.
	agyClient := FromGeminiFunctions([]GeminiFunctionDeclaration{{
		Name:        "run_command",
		Description: "shell",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"CommandLine":{"type":"string"}}}`),
	}})
	if ToClaudeTools(agyClient)[0].Name != "run_command" {
		t.Fatalf("agy→claude name=%s", ToClaudeTools(agyClient)[0].Name)
	}
	if ToCodexResponsesTools(agyClient)[0].Name != "run_command" {
		t.Fatalf("agy→codex name=%s", ToCodexResponsesTools(agyClient)[0].Name)
	}
}
