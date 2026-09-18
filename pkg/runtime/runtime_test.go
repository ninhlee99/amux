package runtime_test

import (
	"strings"
	"testing"

	"amux-accounts/pkg/runtime"
	"amux-accounts/pkg/types"
)

func TestRuntimeManifest_DynamicDiscovery(t *testing.T) {
	req := &types.ChatRequest{
		ClientDialect: "antigravity",
		Tools: []types.ToolDef{
			{
				Name:        "run_command",
				Description: "Propose a command to run on behalf of the user.",
				InputSchema: []byte(`{
					"type": "object",
					"required": ["CommandLine", "Cwd", "WaitMsBeforeAsync", "toolAction", "toolSummary"],
					"properties": {
						"CommandLine": {"type": "string", "description": "The exact command line string to execute."},
						"Cwd": {"type": "string", "description": "The current working directory."},
						"WaitMsBeforeAsync": {"type": "integer", "description": "Milliseconds to wait."},
						"toolAction": {"type": "string", "description": "Brief action phrase."},
						"toolSummary": {"type": "string", "description": "Brief noun phrase."}
					}
				}`),
			},
		},
	}

	manifest := runtime.DiscoverManifest(req)
	if manifest.Runtime != "Antigravity" {
		t.Fatalf("expected runtime 'Antigravity', got '%s'", manifest.Runtime)
	}
	if len(manifest.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(manifest.Tools))
	}

	tool := manifest.Tools[0]
	if tool.Name != "run_command" {
		t.Fatalf("expected tool name 'run_command', got '%s'", tool.Name)
	}
	if len(tool.Parameters) != 5 {
		t.Fatalf("expected 5 parameters, got %d", len(tool.Parameters))
	}
	for _, p := range tool.Parameters {
		if !p.Required {
			t.Fatalf("expected parameter %s to be required", p.Name)
		}
	}
}

func TestRuntimeContract_FormatAndRules(t *testing.T) {
	manifest := runtime.FromToolDefs("ClaudeCode", []types.ToolDef{
		{
			Name:        "Bash",
			Description: "Run bash command",
			InputSchema: []byte(`{
				"type": "object",
				"required": ["command"],
				"properties": {
					"command": {"type": "string"}
				}
			}`),
		},
	})

	contract := runtime.BuildRuntimeContract(manifest)
	if !strings.Contains(contract, "STRICT RUNTIME CONTRACT") {
		t.Fatalf("missing strict runtime contract header")
	}
	if !strings.Contains(contract, "Do NOT rename properties") {
		t.Fatalf("missing strict property rename prohibition")
	}
	if !strings.Contains(contract, "Bash:command:string") {
		t.Fatalf("missing tool signature in contract: %s", contract)
	}
	if !strings.Contains(contract, "required: [command:string]") {
		t.Fatalf("missing required schema list in contract: %s", contract)
	}
}

func TestValidationLayer(t *testing.T) {
	toolDef := runtime.NativeToolDefinition{
		Name: "run_command",
		Parameters: []runtime.ToolParameter{
			{Name: "CommandLine", Type: "string", Required: true},
			{Name: "Cwd", Type: "string", Required: true},
			{Name: "WaitMsBeforeAsync", Type: "integer", Required: true},
		},
	}

	// 1. Invalid call (legacy mistake: "command" instead of "CommandLine", missing Cwd)
	invalidCall := types.ToolCall{
		Name:      "run_command",
		Arguments: `{"command": "git status"}`,
	}
	err := runtime.ValidateToolCall(invalidCall, toolDef)
	if err == nil {
		t.Fatalf("expected validation error for invalid tool call")
	}
	if len(err.MissingRequired) != 3 {
		t.Fatalf("expected 3 missing required fields, got %v", err.MissingRequired)
	}

	// 2. Valid call matching native schema
	validCall := types.ToolCall{
		Name: "run_command",
		Arguments: `{
			"CommandLine": "git status",
			"Cwd": ".",
			"WaitMsBeforeAsync": 1000
		}`,
	}
	if err := runtime.ValidateToolCall(validCall, toolDef); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestScenario_NewMCPToolWithoutCoreChanges(t *testing.T) {
	// A new MCP tool arrives dynamically
	mcpTool := types.ToolDef{
		Name:        "mcp__postgres__query",
		Description: "Execute a read-only SQL query",
		InputSchema: []byte(`{
			"type": "object",
			"required": ["sql"],
			"properties": {
				"sql": {"type": "string", "description": "SQL query string"}
			}
		}`),
	}

	manifest := runtime.FromToolDefs("CustomMCP", []types.ToolDef{mcpTool})
	contract := runtime.BuildRuntimeContract(manifest)

	if !strings.Contains(contract, "mcp__postgres__query:sql:string") {
		t.Fatalf("contract should automatically format new MCP tool: %s", contract)
	}

	// Native validation works out of the box
	tc := types.ToolCall{
		Name:      "mcp__postgres__query",
		Arguments: `{"sql": "SELECT 1;"}`,
	}
	err := runtime.ValidateToolCall(tc, manifest.Tools[0])
	if err != nil {
		t.Fatalf("valid MCP tool call should pass: %v", err)
	}

	badTc := types.ToolCall{
		Name:      "mcp__postgres__query",
		Arguments: `{"query": "SELECT 1;"}`,
	}
	if err := runtime.ValidateToolCall(badTc, manifest.Tools[0]); err == nil {
		t.Fatalf("bad MCP tool call should fail validation")
	}
}

func TestAntiHallucination_RejectCrossIDETools(t *testing.T) {
	// 1. Antigravity environment
	agyReq := &types.ChatRequest{
		ClientDialect: "gemini",
		Tools: []types.ToolDef{
			{
				Name:        "run_command",
				InputSchema: []byte(`{"type":"object","required":["CommandLine","Cwd","WaitMsBeforeAsync","toolAction","toolSummary"],"properties":{"CommandLine":{"type":"string"},"Cwd":{"type":"string"},"WaitMsBeforeAsync":{"type":"integer"},"toolAction":{"type":"string"},"toolSummary":{"type":"string"}}}`),
			},
			{
				Name:        "view_file",
				InputSchema: []byte(`{"type":"object","required":["AbsolutePath","toolAction","toolSummary"],"properties":{"AbsolutePath":{"type":"string"},"toolAction":{"type":"string"},"toolSummary":{"type":"string"}}}`),
			},
		},
	}
	agyManifest := runtime.DiscoverManifest(agyReq)
	if agyManifest.Runtime != "Antigravity" {
		t.Fatalf("expected Antigravity runtime, got %s", agyManifest.Runtime)
	}

	// Model erroneously hallucinates Claude's Bash tool in Antigravity environment
	hallucinatedBash := []types.ToolCall{
		{Name: "Bash", Arguments: `{"command":"git status"}`},
	}
	errs := runtime.ValidateCallsAgainstManifest(hallucinatedBash, agyManifest)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error rejecting hallucinated Bash in Antigravity, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "not supported in Antigravity") {
		t.Fatalf("expected rejection message, got %s", errs[0].Message)
	}

	// Valid AGY tool call
	validAgyCall := []types.ToolCall{
		{
			Name: "run_command",
			Arguments: `{
				"CommandLine": "git status",
				"Cwd": ".",
				"WaitMsBeforeAsync": 1000,
				"toolAction": "Checking git status",
				"toolSummary": "Git status check"
			}`,
		},
	}
	if errs := runtime.ValidateCallsAgainstManifest(validAgyCall, agyManifest); len(errs) > 0 {
		t.Fatalf("valid AGY call failed validation: %v", errs[0])
	}

	// 2. Claude Code environment
	claudeReq := &types.ChatRequest{
		ClientDialect: "claude",
		Tools: []types.ToolDef{
			{
				Name:        "Bash",
				InputSchema: []byte(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
			},
		},
	}
	claudeManifest := runtime.DiscoverManifest(claudeReq)
	if claudeManifest.Runtime != "ClaudeCode" {
		t.Fatalf("expected ClaudeCode runtime, got %s", claudeManifest.Runtime)
	}

	// Model erroneously hallucinates AGY's run_command in Claude Code environment
	hallucinatedRunCmd := []types.ToolCall{
		{Name: "run_command", Arguments: `{"CommandLine":"git status"}`},
	}
	errsClaude := runtime.ValidateCallsAgainstManifest(hallucinatedRunCmd, claudeManifest)
	if len(errsClaude) != 1 {
		t.Fatalf("expected 1 error rejecting hallucinated run_command in ClaudeCode, got %d", len(errsClaude))
	}
	if !strings.Contains(errsClaude[0].Message, "not supported in ClaudeCode") {
		t.Fatalf("expected rejection message, got %s", errsClaude[0].Message)
	}
}
