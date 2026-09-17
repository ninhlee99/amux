package tools_test

import (
	"encoding/json"
	"testing"

	"amux-accounts/pkg/tools"
)

func TestUniversalTool_JSONSchemaPreservation(t *testing.T) {
	// Complex schema with properties, required, enum, items, oneOf, default, additionalProperties
	rawAnthropicTools := []byte(`{
		"tools": [
			{
				"name": "manage_inventory",
				"description": "Updates product inventory levels",
				"input_schema": {
					"type": "object",
					"properties": {
						"sku": {"type": "string", "description": "Stock keeping unit"},
						"quantity": {"type": "integer", "default": 0},
						"status": {"type": "string", "enum": ["in_stock", "low_stock", "out_of_stock"]},
						"tags": {
							"type": "array",
							"items": {"type": "string"}
						},
						"location": {
							"oneOf": [
								{"type": "string"},
								{"type": "object", "properties": {"warehouse_id": {"type": "string"}}}
							]
						}
					},
					"required": ["sku", "quantity"],
					"additionalProperties": false
				}
			}
		]
	}`)

	anthropicAdapter := tools.NewAnthropicToolAdapter()
	universalTools, err := anthropicAdapter.ToUniversalTools(rawAnthropicTools)
	if err != nil {
		t.Fatalf("ToUniversalTools error: %v", err)
	}

	if len(universalTools) != 1 {
		t.Fatalf("expected 1 universal tool, got %d", len(universalTools))
	}

	uTool := universalTools[0]
	if uTool.Name != "manage_inventory" {
		t.Errorf("tool name mismatch: %s", uTool.Name)
	}

	// Verify all schema constraints survived 100%
	schema := uTool.InputSchema
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing properties in schema")
	}
	if _, ok := props["sku"]; !ok {
		t.Errorf("missing sku property")
	}
	if _, ok := props["quantity"]; !ok {
		t.Errorf("missing quantity property")
	}

	reqList, ok := schema["required"].([]interface{})
	if !ok || len(reqList) != 2 {
		t.Errorf("expected 2 required fields, got %v", schema["required"])
	}

	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties was not preserved: %v", schema["additionalProperties"])
	}

	// Convert UniversalTool to OpenAI schema
	openAIAdapter := tools.NewOpenAIToolAdapter()
	openaiOutput, err := openAIAdapter.FromUniversalTools(universalTools)
	if err != nil {
		t.Fatalf("FromUniversalTools OpenAI error: %v", err)
	}

	oBytes, _ := json.Marshal(openaiOutput)
	reparsedUniversal, err := openAIAdapter.ToUniversalTools(oBytes)
	if err != nil {
		t.Fatalf("reparse OpenAI tools error: %v", err)
	}

	reprops := reparsedUniversal[0].InputSchema["properties"].(map[string]interface{})
	if _, ok := reprops["sku"]; !ok {
		t.Errorf("sku missing after round-trip through OpenAI adapter")
	}
}

func TestUniversalTool_MCP_Bidirectional(t *testing.T) {
	mcpManifest := []byte(`{
		"tools": [
			{
				"name": "query_database",
				"description": "Executes SQL query",
				"inputSchema": {
					"type": "object",
					"properties": {
						"query": {"type": "string"}
					},
					"required": ["query"]
				}
			}
		]
	}`)

	mcpAdapter := tools.NewMCPToolAdapter()
	universalTools, err := mcpAdapter.ToUniversalTools(mcpManifest)
	if err != nil {
		t.Fatalf("ToUniversalTools MCP error: %v", err)
	}

	if len(universalTools) != 1 || universalTools[0].Name != "query_database" {
		t.Fatalf("unexpected universal tools from MCP: %v", universalTools)
	}

	// Test MCP tool invocation conversion
	mcpInvocation := map[string]interface{}{
		"params": map[string]interface{}{
			"name": "query_database",
			"arguments": map[string]interface{}{
				"query": "SELECT * FROM orders;",
			},
		},
	}

	calls, err := mcpAdapter.ToUniversalToolCalls(mcpInvocation)
	if err != nil {
		t.Fatalf("ToUniversalToolCalls MCP error: %v", err)
	}
	if len(calls) != 1 || calls[0].Arguments["query"] != "SELECT * FROM orders;" {
		t.Fatalf("unexpected universal tool call: %v", calls)
	}
}

func TestUniversalTool_TextLoopParser(t *testing.T) {
	text := `I will now check the files.
<tool_call>
{"name": "Bash", "arguments": {"command": "git status"}}
</tool_call>
Here is the status.`

	tlAdapter := tools.NewTextLoopToolAdapter()
	calls := tlAdapter.ParseTextCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 parsed call from text loop, got %d", len(calls))
	}
	if calls[0].Name != "Bash" || calls[0].Arguments["command"] != "git status" {
		t.Fatalf("unexpected call parsed: %v", calls[0])
	}
}

func TestProtectedSyntax_IntegrityCheck(t *testing.T) {
	payload := `
<merchant_data>
{"store":"gid://shopify/Shop/123456","order_id":"gid://shopify/Order/987654321"}
</merchant_data>
<thinking>
Analyzing git diff for commit a1b2c3d4e5f67890123456789abcdef012345678
</thinking>
--- a/pkg/tools/dialect.go
+++ b/pkg/tools/dialect.go
@@ -1,5 +1,5 @@
+Bash(command="git status")
`

	violations := tools.AssertProtectedSyntaxIntegrity(payload, payload)
	if len(violations) > 0 {
		t.Fatalf("expected 0 violations on identical payload, got: %v", violations)
	}

	// Test mangled payload
	mangled := "Cleaned content without tags"
	badViolations := tools.AssertProtectedSyntaxIntegrity(payload, mangled)
	if len(badViolations) == 0 {
		t.Fatalf("expected violations on mangled payload")
	}
}
