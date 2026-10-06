package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestParseWebTools_AMUXAndBash(t *testing.T) {
	defs := []types.ToolDef{{Name: "Bash"}, {Name: "Read"}}
	text := `
I'll list files.
<<<AMUX_TOOL name="Read" id="toolu_web_1">>>
{"path":"README.md"}
<<<END_AMUX_TOOL>>>
`
	calls := ParseWebTools(text, defs)
	if len(calls) != 1 || calls[0].Name != "Read" {
		t.Fatalf("amux block: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "README.md") {
		t.Fatalf("args: %s", calls[0].Arguments)
	}

	xml := ParseWebTools(`<tool_call>
{"name": "Bash", "arguments": {"command": "git status"}}
</tool_call>`, defs)
	if len(xml) != 1 || xml[0].Name != "Bash" || !strings.Contains(xml[0].Arguments, "git status") {
		t.Fatalf("xml tool_call: %+v", xml)
	}

	bash := ParseWebTools("run:\n```bash\ngit diff -- README.md\n```\n", defs)
	if len(bash) != 1 || bash[0].Name != "Bash" {
		t.Fatalf("bash fence: %+v", bash)
	}
	if !strings.Contains(bash[0].Arguments, "git diff") {
		t.Fatalf("bash args: %s", bash[0].Arguments)
	}
}

func TestParseWebTools_DialectAliases(t *testing.T) {
	// Cursor-style catalog: model emits Bash/Read → map to client names.
	defs := []types.ToolDef{
		{Name: "run_terminal_command", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
		{Name: "read_file", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
	}
	xml := ParseWebTools(`<tool_call>
{"name": "Bash", "arguments": {"command": "ls"}}
</tool_call>
<tool_call>
{"name": "Read", "arguments": {"path": "a.go"}}
</tool_call>`, defs)
	if len(xml) != 2 {
		t.Fatalf("want 2 calls, got %+v", xml)
	}
	if xml[0].Name != "run_terminal_command" {
		t.Fatalf("bash alias → %q", xml[0].Name)
	}
	if xml[1].Name != "read_file" {
		t.Fatalf("read alias → %q", xml[1].Name)
	}
	if !strings.Contains(xml[1].Arguments, "file_path") {
		t.Fatalf("path coerced to file_path: %s", xml[1].Arguments)
	}
	fence := ParseWebTools("```bash\necho hi\n```", defs)
	if len(fence) != 1 || fence[0].Name != "run_terminal_command" {
		t.Fatalf("bash fence alias: %+v", fence)
	}
}

func TestFinalizeWebToolCalls_AllowProseDropsFence(t *testing.T) {
	defs := []types.ToolDef{{Name: "Bash"}, {Name: "Read"}}
	hist := []types.ChatMessage{
		{Role: "assistant", ToolCalls: []types.ToolCall{{Name: "Read", Arguments: `{"path":"a.go"}`}}},
		{Role: "tool", Content: "ok"},
	}
	// Incomplete checklist + prior tools → prose path; bash fence must not invent calls.
	text := "Need to verify remaining cases after the Read above.\n```bash\ngit status\n```\n"
	calls, forced := FinalizeWebToolCalls(text, defs, hist)
	if forced {
		t.Fatal("allowProse must not force")
	}
	if len(calls) != 0 {
		t.Fatalf("allowProse dropped fence heuristics, got %+v", calls)
	}
}

func TestSchemaKeyTypes_RequiredNeverTruncated(t *testing.T) {
	raw := []byte(`{"required":["a","b","c","d","e","f","g"],"properties":{"a":{"type":"string"},"b":{"type":"string"},"c":{"type":"string"},"d":{"type":"string"},"e":{"type":"string"},"f":{"type":"string"},"g":{"type":"string"},"opt":{"type":"number"}}}`)
	got := schemaKeyTypes(raw, 1)
	if len(got) < 7 {
		t.Fatalf("all required kept, got %v", got)
	}
	if !strings.Contains(strings.Join(got, ","), "opt:number") {
		t.Fatalf("one optional after required: %v", got)
	}
}

func TestCatalogLine_ExpandedOptionalProps(t *testing.T) {
	raw := []byte(`{"properties":{"opt1":{"type":"string"},"opt2":{"type":"string"},"opt3":{"type":"string"},"opt4":{"type":"string"},"opt5":{"type":"string"},"opt6":{"type":"string"},"opt7":{"type":"string"},"opt8":{"type":"string"}}}`)
	line := catalogLine(types.ToolDef{Name: "ComplexTool", InputSchema: raw})
	if !strings.Contains(line, "opt7:string") || !strings.Contains(line, "opt8:string") {
		t.Fatalf("expected catalogLine to retain optional properties beyond 6, got: %s", line)
	}
}

func TestParseWebTools_RejectsUnknown(t *testing.T) {
	defs := []types.ToolDef{{Name: "Read"}}
	calls := ParseWebTools("```bash\nrm -rf /\n```", defs)
	if len(calls) != 0 {
		t.Fatalf("bash must not map when Bash not in catalog: %+v", calls)
	}
}

func TestFormatToolCalls(t *testing.T) {
	got := FormatToolCalls([]types.ToolCall{
		{Name: "Bash", Arguments: `{"command":"git diff -- README.md"}`},
	})
	if !strings.Contains(got, "Bash") || !strings.Contains(got, "git diff") {
		t.Fatalf("format: %s", got)
	}
}

func TestWebPreamble_IncludesSchemaAndMCPRule(t *testing.T) {
	got := WebPreamble([]types.ToolDef{
		{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: []byte(`{"type":"object","required":["file_path"],"properties":{"file_path":{"type":"string"},"offset":{"type":"number"}}}`),
		},
		{
			Name:        "mcp__github__list_prs",
			InputSchema: []byte(`{"type":"object","required":["repo"],"properties":{"repo":{"type":"string"}}}`),
		},
		{Name: "Skill", InputSchema: []byte(`{"type":"object","required":["skill"],"properties":{"skill":{"type":"string"}}}`)},
	})
	if !strings.Contains(got, "CATALOG") || !strings.Contains(got, "<tool_call>") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "Read:file_path:string") {
		t.Fatal("read schema", got)
	}
	if !strings.Contains(got, "mcp__github__list_prs:repo:string") {
		t.Fatal("mcp schema", got)
	}
	if !strings.Contains(got, "Skill:skill:string") {
		t.Fatal("skill schema", got)
	}
	if strings.Contains(got, "Read a file") {
		t.Fatal("descriptions waste tokens")
	}
	if strings.Contains(got, "FEW-SHOT") || strings.Contains(got, "Example 1") {
		t.Fatal("few-shot must stay out of preamble")
	}
}

func TestWebCatalogOnly_NoRulesEssay(t *testing.T) {
	got := WebCatalogOnly([]types.ToolDef{{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)}})
	if !strings.Contains(got, "CATALOG") || !strings.Contains(got, "Bash:command:string") {
		t.Fatal(got)
	}
	if strings.Contains(got, "Coding-agent backend") {
		t.Fatal("catalog-only must omit full preamble")
	}
}

func TestWebCloser_ForbidsLackOfTools(t *testing.T) {
	c := WebCloser()
	if !strings.Contains(c, "<tool_call>") || !strings.Contains(c, "paste") {
		t.Fatal(c)
	}
}

func TestWrapWebStream_RefusalPassesThroughVerbatim(t *testing.T) {
	inner := make(chan types.StreamChunk, 2)
	inner <- types.StreamChunk{Content: "Chưa đọc được repo, không mount. Gửi cho tôi README."}
	close(inner)
	out := wrapWebStream("chatgpt:01", []types.ToolDef{{Name: "Read"}, {Name: "Bash"}}, nil, inner)
	var calls []types.ToolCall
	var content string
	for ch := range out {
		content += ch.Content
		if len(ch.ToolCalls) > 0 {
			calls = append(calls, ch.ToolCalls...)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("expected 0 forced tool calls on plain refusal, got %+v", calls)
	}
	if !strings.Contains(content, "Chưa đọc được repo") {
		t.Fatalf("expected clean refusal text to pass through, got %q", content)
	}
}

func TestStripWebToolMarkup(t *testing.T) {
	in := "thinking\n```bash\nls\n```\ndone"
	got := StripWebToolMarkup(in)
	if strings.Contains(got, "ls") {
		t.Fatalf("fence left: %q", got)
	}
	if !strings.Contains(got, "thinking") {
		t.Fatalf("lost prose: %q", got)
	}
}


// TestParseWebTools_DynamicSchemaArbitraryNestedParams verifies the web
// tool-call emulator does not hardcode tool names or argument shapes: an
// arbitrary, never-before-seen tool ("merchant_lookup") with deeply nested
// object/array arguments must survive the ```tool_call fenced-JSON
// extraction with zero data loss, exactly as any built-in tool would.
func TestParseWebTools_DynamicSchemaArbitraryNestedParams(t *testing.T) {
	defs := []types.ToolDef{{
		Name:        "merchant_lookup",
		Description: "Look up merchant/variant fulfillment info",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"merchant": {
					"type": "object",
					"properties": {
						"store_gid": {"type": "string"},
						"variant_gid": {"type": "string"}
					}
				},
				"fulfillment_channels": {
					"type": "array",
					"items": {"type": "string", "enum": ["pickup", "ship", "digital"]}
				}
			}
		}`),
	}}

	wantArgs := map[string]any{
		"merchant": map[string]any{
			"store_gid":   "gid://shopify/Shop/123",
			"variant_gid": "gid://shopify/ProductVariant/456",
		},
		"fulfillment_channels": []any{"pickup", "ship"},
	}
	argsJSON, err := json.Marshal(wantArgs)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	text := "```tool_call\n" +
		`{"name": "merchant_lookup", "arguments": ` + string(argsJSON) + `}` +
		"\n```"

	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(calls), calls)
	}
	if calls[0].Name != "merchant_lookup" {
		t.Fatalf("expected merchant_lookup, got %q", calls[0].Name)
	}

	var gotArgs map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &gotArgs); err != nil {
		t.Fatalf("unmarshal round-tripped arguments: %v (raw: %s)", err, calls[0].Arguments)
	}
	if !reflect.DeepEqual(wantArgs, gotArgs) {
		t.Fatalf("nested arguments not preserved:\nwant: %#v\ngot:  %#v", wantArgs, gotArgs)
	}

	// SSE reconstruction: FormatToolCalls / the tool_use event path must
	// carry the same nested arguments through to the client-facing event.
	formatted := FormatToolCalls(calls)
	if !strings.Contains(formatted, "gid://shopify/Shop/123") {
		t.Fatalf("formatted tool call lost nested merchant data: %s", formatted)
	}
}

// TestParseWebTools_UnescapedQuotesInsideBashCommand is a regression test
// for a real bug: the ```tool_call fenced-block path called a bare
// json.Unmarshal and silently dropped the entire call whenever a web model
// produced a shell command containing its own unescaped double quotes
// (extremely common — e.g. `gh issue create --title "..." --body "..."`),
// since it never routed through parseToolCallJSON's repair/fallback logic
// the way the <tool_call>/[tool_call ...] paths already did.
func TestParseWebTools_UnescapedQuotesInsideBashCommand(t *testing.T) {
	cmd := "gh issue create --repo <repo-name> \\\n" +
		"      --title \"bug(fix): infinite tool loop when verifying findings exhausts rate limits\" \\\n" +
		"      --body \"### Bug description\n" +
		"    When running `/open-test:fix` with multiple PRs or submodules, the agent gets stuck in an infinite tool loop.\n" +
		"\n" +
		"    ### Proposed solution\n" +
		"    1. Add a circuit breaker.\""

	defs := []types.ToolDef{{Name: "Bash"}}
	// Hand-rolled JSON simulating a web model that forgot to escape the
	// inner double quotes around --title/--body (a naive text-generation
	// mistake, as opposed to native structured tool-calling which always
	// escapes correctly).
	handRolled := "{\"name\": \"Bash\", \"arguments\": {\"command\": \"" + cmd + "\"}}"
	text := "```tool_call\n" + handRolled + "\n```"

	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 recovered call, got %d: %+v", len(calls), calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %q", calls[0].Name)
	}
	var got struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(calls[0].Arguments), &got); err != nil {
		t.Fatalf("recovered arguments are not valid JSON: %v (raw: %s)", err, calls[0].Arguments)
	}
	if got.Command != cmd {
		t.Fatalf("command not preserved byte-for-byte:\nwant=%q\ngot =%q", cmd, got.Command)
	}
}

func TestParseWebTools_XMLAttributes(t *testing.T) {
	defs := []types.ToolDef{{Name: "Bash"}}

	// Test 1: name attribute in <tool_call>
	text1 := `<tool_call name="Bash">
{"command": "git status"}
</tool_call>`
	calls1 := ParseWebTools(text1, defs)
	if len(calls1) != 1 || calls1[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call, got: %+v", calls1)
	}
	if !strings.Contains(calls1[0].Arguments, "git status") {
		t.Fatalf("expected git status argument, got: %s", calls1[0].Arguments)
	}

	// Test 2: name and id attributes in <tool_call>
	text2 := `<tool_call name="Bash" id="toolu_custom_99">
{"command": "echo hello"}
</tool_call>`
	calls2 := ParseWebTools(text2, defs)
	if len(calls2) != 1 || calls2[0].Name != "Bash" || calls2[0].ID != "toolu_custom_99" {
		t.Fatalf("expected 1 Bash call with id toolu_custom_99, got: %+v", calls2)
	}

	// Test 3: Markdown fence inside <tool_call>
	text3 := "<tool_call>\n```json\n{\n  \"name\": \"Bash\",\n  \"arguments\": {\"command\": \"ls -lh\"}\n}\n```\n</tool_call>"
	calls3 := ParseWebTools(text3, defs)
	if len(calls3) != 1 || calls3[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from fenced block, got: %+v", calls3)
	}
	if !strings.Contains(calls3[0].Arguments, "ls -lh") {
		t.Fatalf("expected ls -lh argument, got: %s", calls3[0].Arguments)
	}

	// Test 4: Anthropic <invoke> XML format with parameters
	text4 := `<invoke name="Bash">
<parameter name="command">git diff</parameter>
</invoke>`
	calls4 := ParseWebTools(text4, defs)
	if len(calls4) != 1 || calls4[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from invoke, got: %+v", calls4)
	}
	if !strings.Contains(calls4[0].Arguments, "git diff") {
		t.Fatalf("expected git diff argument, got: %s", calls4[0].Arguments)
	}

	// Test 5: Bracket alternate syntax [tool_call Bash {...}]
	text5 := `[tool_call Bash {"command":"pwd"}]`
	calls5 := ParseWebTools(text5, defs)
	if len(calls5) != 1 || calls5[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from bracket alt, got: %+v", calls5)
	}
	if !strings.Contains(calls5[0].Arguments, "pwd") {
		t.Fatalf("expected pwd argument, got: %s", calls5[0].Arguments)
	}
}

func TestParseWebTools_LenientMultiSchemaFormats(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
		{Name: "Read", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
	}

	// 1. Gemini parameters format
	geminiText := `<tool_call>
{"name": "Bash", "parameters": {"command": "git log -n 5"}}
</tool_call>`
	geminiCalls := ParseWebTools(geminiText, defs)
	if len(geminiCalls) != 1 || geminiCalls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from Gemini parameters format, got: %+v", geminiCalls)
	}
	if !strings.Contains(geminiCalls[0].Arguments, "git log -n 5") {
		t.Fatalf("expected git log in arguments, got: %s", geminiCalls[0].Arguments)
	}

	// 2. Flat JSON format (no arguments wrapper)
	flatText := `<tool_call>
{"name": "Bash", "command": "npm test"}
</tool_call>`
	flatCalls := ParseWebTools(flatText, defs)
	if len(flatCalls) != 1 || flatCalls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from flat JSON format, got: %+v", flatCalls)
	}
	if !strings.Contains(flatCalls[0].Arguments, "npm test") {
		t.Fatalf("expected npm test in arguments, got: %s", flatCalls[0].Arguments)
	}

	// 3. OpenAI function calling format with stringified arguments
	openaiStrText := `<tool_call>
{"function": {"name": "Read", "arguments": "{\"file_path\": \"main.go\"}"}}
</tool_call>`
	openaiStrCalls := ParseWebTools(openaiStrText, defs)
	if len(openaiStrCalls) != 1 || openaiStrCalls[0].Name != "Read" {
		t.Fatalf("expected 1 Read call from OpenAI stringified arguments format, got: %+v", openaiStrCalls)
	}
	if !strings.Contains(openaiStrCalls[0].Arguments, "main.go") {
		t.Fatalf("expected main.go in arguments, got: %s", openaiStrCalls[0].Arguments)
	}

	// 4. OpenAI function calling format with object arguments
	openaiObjText := `<tool_call>
{"function": {"name": "Bash", "arguments": {"command": "cargo build"}}}
</tool_call>`
	openaiObjCalls := ParseWebTools(openaiObjText, defs)
	if len(openaiObjCalls) != 1 || openaiObjCalls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from OpenAI object arguments format, got: %+v", openaiObjCalls)
	}
	if !strings.Contains(openaiObjCalls[0].Arguments, "cargo build") {
		t.Fatalf("expected cargo build in arguments, got: %s", openaiObjCalls[0].Arguments)
	}

	// 5. LangChain / Agent action format
	actionText := `<tool_call>
{"action": "Bash", "action_input": {"command": "python -m pytest"}}
</tool_call>`
	actionCalls := ParseWebTools(actionText, defs)
	if len(actionCalls) != 1 || actionCalls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from LangChain action format, got: %+v", actionCalls)
	}
	if !strings.Contains(actionCalls[0].Arguments, "python -m pytest") {
		t.Fatalf("expected python -m pytest in arguments, got: %s", actionCalls[0].Arguments)
	}
}

func TestStripWebToolMarkup_ResidualFences(t *testing.T) {
	input := "Here is my plan:\n```xml\n<tool_call>\n{\"name\": \"Bash\", \"command\": \"ls\"}\n</tool_call>\n```\nAll done."
	stripped := StripWebToolMarkup(input)
	if strings.Contains(stripped, "```") {
		t.Fatalf("expected code fence to be completely stripped, got: %q", stripped)
	}
	if !strings.Contains(stripped, "Here is my plan:") || !strings.Contains(stripped, "All done.") {
		t.Fatalf("expected surrounding prose to be preserved, got: %q", stripped)
	}
}


