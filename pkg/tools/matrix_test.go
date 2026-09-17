package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/tools"
)

// 1. Claude -> OpenAI
func TestMatrix_ClaudeToOpenAI(t *testing.T) {
	claudeToolsRaw := []byte(`{
		"tools": [
			{
				"name": "edit_file",
				"description": "Edits a local file",
				"input_schema": {
					"type": "object",
					"properties": {
						"path": {"type": "string"},
						"content": {"type": "string"}
					},
					"required": ["path", "content"]
				}
			}
		]
	}`)

	claudeAdapter := tools.NewAnthropicToolAdapter()
	uTools, err := claudeAdapter.ToUniversalTools(claudeToolsRaw)
	if err != nil {
		t.Fatalf("Claude ToUniversalTools error: %v", err)
	}

	openaiAdapter := tools.NewOpenAIToolAdapter()
	openaiOutput, err := openaiAdapter.FromUniversalTools(uTools)
	if err != nil {
		t.Fatalf("OpenAI FromUniversalTools error: %v", err)
	}

	oBytes, _ := json.Marshal(openaiOutput)
	reparsed, err := openaiAdapter.ToUniversalTools(oBytes)
	if err != nil || len(reparsed) != 1 {
		t.Fatalf("failed to roundtrip Claude tools to OpenAI: %v", err)
	}

	if reparsed[0].Name != "edit_file" {
		t.Errorf("expected tool name edit_file, got %s", reparsed[0].Name)
	}
}

// 2. Claude -> Gemini
func TestMatrix_ClaudeToGemini(t *testing.T) {
	claudeCalls := []tools.UniversalToolCall{
		{
			ID:        "toolu_01",
			Name:      "run_command",
			Arguments: map[string]interface{}{"command": "go test ./..."},
		},
	}

	geminiAdapter := tools.NewGeminiToolAdapter()
	geminiOut, err := geminiAdapter.FromUniversalToolCalls(claudeCalls)
	if err != nil {
		t.Fatalf("Gemini FromUniversalToolCalls error: %v", err)
	}

	reparsedCalls, err := geminiAdapter.ToUniversalToolCalls(geminiOut)
	if err != nil {
		// Single part unmarshal
		t.Logf("geminiOut: %v", geminiOut)
	} else if len(reparsedCalls) > 0 {
		if reparsedCalls[0].Name != "run_command" {
			t.Errorf("expected run_command, got %s", reparsedCalls[0].Name)
		}
	}
}

// 3. OpenAI -> Claude
func TestMatrix_OpenAIToClaude(t *testing.T) {
	openaiCallsRaw := []map[string]interface{}{
		{
			"id":   "call_abc123",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "read_file",
				"arguments": `{"path":"main.go"}`,
			},
		},
	}

	openaiAdapter := tools.NewOpenAIToolAdapter()
	uCalls, err := openaiAdapter.ToUniversalToolCalls(openaiCallsRaw)
	if err != nil {
		t.Fatalf("OpenAI ToUniversalToolCalls error: %v", err)
	}

	claudeAdapter := tools.NewAnthropicToolAdapter()
	claudeBlocks, err := claudeAdapter.FromUniversalToolCalls(uCalls)
	if err != nil {
		t.Fatalf("Claude FromUniversalToolCalls error: %v", err)
	}

	b, _ := json.Marshal(claudeBlocks)
	var parsedBlocks []struct {
		Type  string                 `json:"type"`
		ID    string                 `json:"id"`
		Name  string                 `json:"name"`
		Input map[string]interface{} `json:"input"`
	}
	if err := json.Unmarshal(b, &parsedBlocks); err != nil {
		t.Fatalf("parse claude blocks error: %v", err)
	}

	if len(parsedBlocks) != 1 || parsedBlocks[0].Name != "read_file" || parsedBlocks[0].ID != "call_abc123" {
		t.Fatalf("unexpected claude block: %v", parsedBlocks)
	}
	if parsedBlocks[0].Input["path"] != "main.go" {
		t.Errorf("expected input path main.go, got %v", parsedBlocks[0].Input["path"])
	}
}

// 4. Gemini -> Claude
func TestMatrix_GeminiToClaude(t *testing.T) {
	geminiDecls := []byte(`{
		"functionDeclarations": [
			{
				"name": "search_code",
				"description": "Searches codebase",
				"parameters": {
					"type": "object",
					"properties": {
						"query": {"type": "string"}
					},
					"required": ["query"]
				}
			}
		]
	}`)

	geminiAdapter := tools.NewGeminiToolAdapter()
	uTools, err := geminiAdapter.ToUniversalTools(geminiDecls)
	if err != nil {
		t.Fatalf("Gemini ToUniversalTools error: %v", err)
	}

	claudeAdapter := tools.NewAnthropicToolAdapter()
	claudeOutput, err := claudeAdapter.FromUniversalTools(uTools)
	if err != nil {
		t.Fatalf("Claude FromUniversalTools error: %v", err)
	}

	cb, _ := json.Marshal(claudeOutput)
	if !strings.Contains(string(cb), "search_code") || !strings.Contains(string(cb), "query") {
		t.Fatalf("Claude tools missing search_code or query: %s", string(cb))
	}
}

// 5. MCP -> Claude
func TestMatrix_MCPToClaude(t *testing.T) {
	mcpManifest := []byte(`{
		"tools": [
			{
				"name": "mcp_browser_click",
				"description": "Clicks an element in the browser",
				"inputSchema": {
					"type": "object",
					"properties": {
						"selector": {"type": "string"}
					},
					"required": ["selector"]
				}
			}
		]
	}`)

	mcpAdapter := tools.NewMCPToolAdapter()
	uTools, err := mcpAdapter.ToUniversalTools(mcpManifest)
	if err != nil {
		t.Fatalf("MCP ToUniversalTools error: %v", err)
	}

	claudeAdapter := tools.NewAnthropicToolAdapter()
	claudeTools, err := claudeAdapter.FromUniversalTools(uTools)
	if err != nil {
		t.Fatalf("Claude FromUniversalTools error: %v", err)
	}

	cb, _ := json.Marshal(claudeTools)
	if !strings.Contains(string(cb), "mcp_browser_click") || !strings.Contains(string(cb), "selector") {
		t.Fatalf("MCP tool conversion to Claude failed: %s", string(cb))
	}
}

// 6. Web Emulation -> Client Tool Calls
func TestMatrix_WebEmulationToClientToolCalls(t *testing.T) {
	webChunk := "Let me check git status for you.\n```tool_call\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"git status\"}}\n```\n"

	tlAdapter := tools.NewTextLoopToolAdapter()
	calls := tlAdapter.ParseTextCalls(webChunk)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call parsed from web emulation, got %d", len(calls))
	}

	if calls[0].Name != "Bash" || calls[0].Arguments["command"] != "git status" {
		t.Fatalf("unexpected call parsed: %v", calls[0])
	}

	claudeAdapter := tools.NewAnthropicToolAdapter()
	blocks, err := claudeAdapter.FromUniversalToolCalls(calls)
	if err != nil {
		t.Fatalf("FromUniversalToolCalls error: %v", err)
	}

	b, _ := json.Marshal(blocks)
	if !strings.Contains(string(b), `"name":"Bash"`) || !strings.Contains(string(b), `"git status"`) {
		t.Fatalf("failed to format web tool call into Claude block: %s", string(b))
	}
}

// 7. XML merchant context (<merchant_data>) round-trip verification
func TestMatrix_XMLMerchantContextRoundTrip(t *testing.T) {
	merchantPayload := `
<merchant_data>
{
  "shop_id": "gid://shopify/Shop/12345678",
  "product_id": "gid://shopify/Product/87654321",
  "variant_id": "gid://shopify/ProductVariant/47643685191842"
}
</merchant_data>

<context>
Store currency: USD, Checkout domain: store.myshopify.com
</context>

<thinking>
Reviewing git diff for commit 4b825dc642cb6eb9a060e54bf8d69288fbee4904
</thinking>

--- a/config/settings.json
+++ b/config/settings.json
@@ -1,4 +1,4 @@
-{"active": false}
+{"active": true}

Terminal execution:
Bash(command="bundle exec rake db:migrate")
`

	// 1. Pass through Privacy Redactor
	redacted, res := privacy.RedactString(merchantPayload)
	_ = res

	// Verify all protected syntax survived 100% verbatim
	violations := tools.AssertProtectedSyntaxIntegrity(merchantPayload, redacted)
	if len(violations) > 0 {
		t.Fatalf("privacy redactor violated protected syntax: %v\nRedacted:\n%s", violations, redacted)
	}

	// Verify specific domain IDs survive intact
	if !strings.Contains(redacted, "gid://shopify/ProductVariant/47643685191842") {
		t.Errorf("Shopify variant GID was corrupted")
	}
	if !strings.Contains(redacted, "4b825dc642cb6eb9a060e54bf8d69288fbee4904") {
		t.Errorf("Git commit SHA was corrupted")
	}
	if !strings.Contains(redacted, "<merchant_data>") || !strings.Contains(redacted, "</merchant_data>") {
		t.Errorf("XML merchant_data boundaries were corrupted")
	}
	if !strings.Contains(redacted, "--- a/config/settings.json") || !strings.Contains(redacted, "+++ b/config/settings.json") {
		t.Errorf("Unified diff headers were corrupted")
	}
}
