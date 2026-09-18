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
