package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestToolGateway_RunCommand_AGYStrict(t *testing.T) {
	// Simulate AGY's strict run_command declaration
	agySchema := json.RawMessage(`{
		"type": "OBJECT",
		"properties": {
			"CommandLine": {"type": "STRING"},
			"Cwd": {"type": "STRING"},
			"WaitMsBeforeAsync": {"type": "INTEGER"},
			"toolAction": {"type": "STRING"},
			"toolSummary": {"type": "STRING"}
		},
		"required": ["Cwd", "WaitMsBeforeAsync", "CommandLine", "toolSummary", "toolAction"]
	}`)

	defs := []types.ToolDef{
		{
			Name:        "run_command",
			Description: "Run shell command",
			InputSchema: agySchema,
		},
	}

	// Model emits standard Claude format: {"command": "git status"}
	incomingCalls := []types.ToolCall{
		{
			ID:        "call_1",
			Name:      "run_command",
			Arguments: `{"command":"git status"}`,
		},
	}

	normalized := tools.NormalizeToolCalls(incomingCalls, defs, tools.DialectGemini)
	if len(normalized) != 1 {
		t.Fatalf("expected 1 call, got %d", len(normalized))
	}

	call := normalized[0]
	if call.Name != "run_command" {
		t.Errorf("expected run_command, got %s", call.Name)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &m); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}

	// Verify CommandLine was remapped from command
	if m["CommandLine"] != "git status" {
		t.Errorf("expected CommandLine 'git status', got %v", m["CommandLine"])
	}

	// Verify command was STRIPPED to avoid validator reject
	if _, hasCommand := m["command"]; hasCommand {
		t.Errorf("expected 'command' to be deleted, but still present in %v", m)
	}

	// Verify all required properties were injected
	if m["Cwd"] != "." {
		t.Errorf("expected Cwd '.', got %v", m["Cwd"])
	}
	if v, ok := m["WaitMsBeforeAsync"].(float64); !ok || int(v) != 10000 {
		t.Errorf("expected WaitMsBeforeAsync 10000, got %v", m["WaitMsBeforeAsync"])
	}
	if m["toolAction"] == "" || !strings.Contains(m["toolAction"].(string), "Running") {
		t.Errorf("expected toolAction, got %v", m["toolAction"])
	}
	if m["toolSummary"] == "" {
		t.Errorf("expected toolSummary, got %v", m["toolSummary"])
	}
}

func TestToolGateway_BashToRunCommand(t *testing.T) {
	// Client is AGY with run_command
	defs := []types.ToolDef{
		{
			Name: "run_command",
			InputSchema: json.RawMessage(`{
				"type": "OBJECT",
				"properties": {
					"CommandLine": {"type": "STRING"},
					"Cwd": {"type": "STRING"},
					"WaitMsBeforeAsync": {"type": "INTEGER"},
					"toolAction": {"type": "STRING"},
					"toolSummary": {"type": "STRING"}
				},
				"required": ["CommandLine", "Cwd", "WaitMsBeforeAsync", "toolAction", "toolSummary"]
			}`),
		},
	}

	// Model emits Bash tool
	incomingCalls := []types.ToolCall{
		{
			ID:        "call_bash",
			Name:      "Bash",
			Arguments: `{"command":"ls -la"}`,
		},
	}

	normalized := tools.NormalizeToolCalls(incomingCalls, defs, tools.DialectGemini)
	if len(normalized) != 1 {
		t.Fatalf("expected 1 call, got %d", len(normalized))
	}

	call := normalized[0]
	if call.Name != "run_command" {
		t.Errorf("expected Bash to be translated to run_command, got %s", call.Name)
	}

	var m map[string]any
	json.Unmarshal([]byte(call.Arguments), &m)
	if m["CommandLine"] != "ls -la" {
		t.Errorf("expected CommandLine 'ls -la', got %v", m["CommandLine"])
	}
	if _, hasCommand := m["command"]; hasCommand {
		t.Errorf("expected 'command' to be deleted, got %v", m)
	}
}

func TestToolGateway_InvokeSubagent_Packaging(t *testing.T) {
	// Client is AGY with invoke_subagent
	defs := []types.ToolDef{
		{
			Name: "invoke_subagent",
			InputSchema: json.RawMessage(`{
				"type": "OBJECT",
				"properties": {
					"Subagents": {"type": "ARRAY"},
					"toolAction": {"type": "STRING"},
					"toolSummary": {"type": "STRING"}
				},
				"required": ["Subagents"]
			}`),
		},
	}

	// Model emits flat prompt
	incomingCalls := []types.ToolCall{
		{
			ID:        "call_agent",
			Name:      "Agent",
			Arguments: `{"prompt":"Investigate auth flow","description":"Codebase Researcher"}`,
		},
	}

	normalized := tools.NormalizeToolCalls(incomingCalls, defs, tools.DialectGemini)
	if len(normalized) != 1 {
		t.Fatalf("expected 1 call, got %d", len(normalized))
	}

	call := normalized[0]
	if call.Name != "invoke_subagent" {
		t.Errorf("expected invoke_subagent, got %s", call.Name)
	}

	var m map[string]any
	json.Unmarshal([]byte(call.Arguments), &m)

	subs, ok := m["Subagents"].([]any)
	if !ok || len(subs) != 1 {
		t.Fatalf("expected Subagents array with 1 item, got %v", m["Subagents"])
	}

	subObj := subs[0].(map[string]any)
	if subObj["Prompt"] != "Investigate auth flow" {
		t.Errorf("expected Prompt 'Investigate auth flow', got %v", subObj["Prompt"])
	}
	if subObj["Role"] != "Codebase Researcher" {
		t.Errorf("expected Role 'Codebase Researcher', got %v", subObj["Role"])
	}
}

func TestNormalizeToolCalls_AutoAbsPathAndLineBounds(t *testing.T) {
	defs := []types.ToolDef{
		{
			Name: "view_file",
			InputSchema: json.RawMessage(`{
				"type": "OBJECT",
				"properties": {
					"AbsolutePath": {"type": "STRING"},
					"toolAction": {"type": "STRING"},
					"toolSummary": {"type": "STRING"}
				},
				"required": ["AbsolutePath"]
			}`),
		},
		{
			Name: "replace_file_content",
			InputSchema: json.RawMessage(`{
				"type": "OBJECT",
				"properties": {
					"TargetFile": {"type": "STRING"},
					"TargetContent": {"type": "STRING"},
					"ReplacementContent": {"type": "STRING"},
					"StartLine": {"type": "INTEGER"},
					"EndLine": {"type": "INTEGER"},
					"AllowMultiple": {"type": "BOOLEAN"},
					"Instruction": {"type": "STRING"},
					"Description": {"type": "STRING"},
					"toolAction": {"type": "STRING"},
					"toolSummary": {"type": "STRING"}
				},
				"required": ["TargetFile", "TargetContent", "ReplacementContent", "StartLine", "EndLine"]
			}`),
		},
	}

	incomingCalls := []types.ToolCall{
		{
			ID:        "call_view",
			Name:      "view_file",
			Arguments: `{"AbsolutePath":"main.go"}`,
		},
		{
			ID:   "call_edit",
			Name: "replace_file_content",
			// Web model omitted StartLine/EndLine
			Arguments: `{"TargetFile":"main.go","TargetContent":"package main","ReplacementContent":"package main"}`,
		},
	}

	normalized := tools.NormalizeToolCalls(incomingCalls, defs, tools.DialectGemini)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(normalized))
	}

	var mView map[string]any
	if err := json.Unmarshal([]byte(normalized[0].Arguments), &mView); err != nil {
		t.Fatalf("unmarshal view arguments: %v", err)
	}
	absPath, _ := mView["AbsolutePath"].(string)
	if !strings.HasPrefix(absPath, "/") {
		t.Fatalf("expected absolute path for AbsolutePath, got %q", absPath)
	}

	var mEdit map[string]any
	if err := json.Unmarshal([]byte(normalized[1].Arguments), &mEdit); err != nil {
		t.Fatalf("unmarshal edit arguments: %v", err)
	}

	// Verify EndLine is NOT 1000000, but bounded to the actual lines of main.go (which has ~10-50 lines)
	endLine, ok := mEdit["EndLine"].(float64)
	if !ok || int(endLine) >= 1000000 {
		t.Fatalf("expected EndLine to be bounded to real file lines, got %v", mEdit["EndLine"])
	}

	// TargetContent "package main" is on line 1 of main.go
	startLine, ok := mEdit["StartLine"].(float64)
	if !ok || int(startLine) != 1 {
		t.Errorf("expected StartLine 1 for 'package main', got %v", mEdit["StartLine"])
	}
}
