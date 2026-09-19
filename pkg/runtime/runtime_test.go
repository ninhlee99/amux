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

func TestRuntimeContract_ReferencedExecutableToolsExistInCatalog(t *testing.T) {
	// Manifest with a subset of tools: only run_command and view_file (no replace_file_content)
	manifest := runtime.FromToolDefs("Antigravity", []types.ToolDef{
		{
			Name:        "run_command",
			InputSchema: []byte(`{"type":"object","required":["CommandLine"],"properties":{"CommandLine":{"type":"string"}}}`),
		},
		{
			Name:        "view_file",
			InputSchema: []byte(`{"type":"object","required":["AbsolutePath"],"properties":{"AbsolutePath":{"type":"string"}}}`),
		},
	})

	contract := runtime.BuildRuntimeContract(manifest)

	// Invariant 1: Available executable tools header contains only the present tools
	if !strings.Contains(contract, "Available executable tools: [run_command, view_file].") {
		t.Fatalf("expected available tools list to match manifest exactly, got:\n%s", contract)
	}

	// Invariant 2: Present tools have guidance
	if !strings.Contains(contract, "Execute shell commands via 'run_command'") {
		t.Fatalf("expected guidance for present run_command, got:\n%s", contract)
	}
	if !strings.Contains(contract, "Inspect files via 'view_file'") {
		t.Fatalf("expected guidance for present view_file, got:\n%s", contract)
	}

	// Invariant 3: Absent tools (e.g. replace_file_content) MUST NOT be instructed
	if strings.Contains(contract, "Edit files via 'replace_file_content'") {
		t.Fatalf("contract instructed using replace_file_content which does not exist in manifest:\n%s", contract)
	}
}

func TestScenario_DynamicFutureTools_AcrossAGY_Codex_Cursor(t *testing.T) {
	testClients := []struct {
		ClientDialect string
		RuntimeName   string
		Tools         []types.ToolDef
	}{
		{
			ClientDialect: "antigravity",
			RuntimeName:   "Antigravity",
			Tools: []types.ToolDef{
				{
					Name:        "future_agy_cloud_deploy",
					Description: "Deploy cloud services",
					InputSchema: []byte(`{"type":"object","required":["serviceName","region"],"properties":{"serviceName":{"type":"string"},"region":{"type":"string"}}}`),
				},
				{
					Name:        "mcp__kubernetes__get_pods",
					Description: "Fetch K8s pods",
					InputSchema: []byte(`{"type":"object","required":["namespace"],"properties":{"namespace":{"type":"string"}}}`),
				},
			},
		},
		{
			ClientDialect: "codex",
			RuntimeName:   "Codex",
			Tools: []types.ToolDef{
				{
					Name:        "codex_custom_sandbox",
					Description: "Execute code in sandbox",
					InputSchema: []byte(`{"type":"object","required":["code","timeout"],"properties":{"code":{"type":"string"},"timeout":{"type":"integer"}}}`),
				},
				{
					Name:        "mcp__github__create_issue",
					Description: "Open GitHub issue",
					InputSchema: []byte(`{"type":"object","required":["title","body"],"properties":{"title":{"type":"string"},"body":{"type":"string"}}}`),
				},
			},
		},
		{
			ClientDialect: "cursor",
			RuntimeName:   "Cursor",
			Tools: []types.ToolDef{
				{
					Name:        "cursor_semantic_symbol_search",
					Description: "Search symbols semantically",
					InputSchema: []byte(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`),
				},
				{
					Name:        "mcp__slack__send_message",
					Description: "Post message to Slack",
					InputSchema: []byte(`{"type":"object","required":["channel","text"],"properties":{"channel":{"type":"string"},"text":{"type":"string"}}}`),
				},
			},
		},
	}

	for _, tc := range testClients {
		t.Run(tc.RuntimeName, func(t *testing.T) {
			req := &types.ChatRequest{
				ClientDialect: tc.ClientDialect,
				Tools:         tc.Tools,
			}

			// 1. Dynamic Discovery without hardcoded knowledge
			manifest := runtime.DiscoverManifest(req)
			if manifest.Runtime != tc.RuntimeName {
				t.Fatalf("expected runtime %s, got %s", tc.RuntimeName, manifest.Runtime)
			}
			if len(manifest.Tools) != len(tc.Tools) {
				t.Fatalf("expected %d tools, got %d", len(tc.Tools), len(manifest.Tools))
			}

			// 2. Strict Contract Generation
			contract := runtime.BuildRuntimeContract(manifest)
			for _, tool := range tc.Tools {
				if !strings.Contains(contract, tool.Name) {
					t.Fatalf("contract missing future tool %s:\n%s", tool.Name, contract)
				}
			}

			// 3. Automated Validation for future tools
			firstTool := manifest.Tools[0]
			validCall := types.ToolCall{
				Name: firstTool.Name,
			}
			if tc.RuntimeName == "Antigravity" {
				validCall.Arguments = `{"serviceName": "amux-svc", "region": "us-central1"}`
			} else if tc.RuntimeName == "Codex" {
				validCall.Arguments = `{"code": "fmt.Println(1)", "timeout": 30}`
			} else {
				validCall.Arguments = `{"query": "BuildRuntimeContract"}`
			}

			if err := runtime.ValidateToolCall(validCall, firstTool); err != nil {
				t.Fatalf("validation failed for valid call to future tool %s: %v", firstTool.Name, err)
			}

			// 4. Invalidation of bad calls
			invalidCall := types.ToolCall{
				Name:      firstTool.Name,
				Arguments: `{"unexpected_param": "bad"}`,
			}
			if err := runtime.ValidateToolCall(invalidCall, firstTool); err == nil {
				t.Fatalf("validation should reject call missing required parameters for %s", firstTool.Name)
			}
		})
	}
}

func TestZeroTools_NoCatalogAndNoBlock(t *testing.T) {
	// A pure conversational chat request with NO tools
	req := &types.ChatRequest{
		ClientDialect: "claude",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Hello, what is Go?"},
		},
		Tools: nil,
	}

	manifest := runtime.DiscoverManifest(req)
	contract := runtime.BuildRuntimeContract(manifest)

	// Invariant: Zero tools MUST return empty contract and never block or fail
	if contract != "" {
		t.Fatalf("expected empty contract when no tools are present, got:\n%s", contract)
	}
}

func TestMultiUserMultiConfig_StrictIsolation(t *testing.T) {
	// User A with Python + K8s tools
	reqA := &types.ChatRequest{
		ClientDialect: "cursor",
		Tools: []types.ToolDef{
			{Name: "python_interpreter", InputSchema: []byte(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`)},
			{Name: "k8s_status", InputSchema: []byte(`{"type":"object","required":["cluster"],"properties":{"cluster":{"type":"string"}}}`)},
		},
	}

	// User B with Go + PostgreSQL tools
	reqB := &types.ChatRequest{
		ClientDialect: "antigravity",
		Tools: []types.ToolDef{
			{Name: "go_test_runner", InputSchema: []byte(`{"type":"object","required":["package"],"properties":{"package":{"type":"string"}}}`)},
			{Name: "postgres_query", InputSchema: []byte(`{"type":"object","required":["sql"],"properties":{"sql":{"type":"string"}}}`)},
		},
	}

	manifestA := runtime.DiscoverManifest(reqA)
	manifestB := runtime.DiscoverManifest(reqB)

	contractA := runtime.BuildRuntimeContract(manifestA)
	contractB := runtime.BuildRuntimeContract(manifestB)

	// Contract A must only contain User A tools
	if !strings.Contains(contractA, "python_interpreter") || !strings.Contains(contractA, "k8s_status") {
		t.Fatalf("contractA missing User A tools")
	}
	if strings.Contains(contractA, "go_test_runner") || strings.Contains(contractA, "postgres_query") {
		t.Fatalf("contractA leaked User B tools")
	}

	// Contract B must only contain User B tools
	if !strings.Contains(contractB, "go_test_runner") || !strings.Contains(contractB, "postgres_query") {
		t.Fatalf("contractB missing User B tools")
	}
	if strings.Contains(contractB, "python_interpreter") || strings.Contains(contractB, "k8s_status") {
		t.Fatalf("contractB leaked User A tools")
	}
}



