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
	// Continuing threads must still restate that tools exist and how to call
	// them, or ChatGPT web answers "I have no tool access" a few turns in.
	if !strings.Contains(got, "<tool_call>") || !strings.Contains(got, "TOOLS LIVE:") {
		t.Fatal("catalog-only must keep the call format reminder:", got)
	}
	if strings.Contains(StripWebToolMarkup("TOOLS LIVE: x\nok"), "TOOLS LIVE") {
		t.Fatal("echoed reminder must be stripped")
	}
}

func TestWebCloser_HasToolCallNotice(t *testing.T) {
	c := WebCloser()
	if !strings.Contains(c, "<tool_call>") || !strings.Contains(c, "[end]") {
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

func TestParseWebTools_ExpandedAliases(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "run_command"},
		{Name: "view_file"},
		{Name: "replace_file_content"},
		{Name: "grep_search"},
		{Name: "find_by_name"},
	}

	// 1. Terminal / bash alias variants
	text := `<tool_call>
{"name": "terminal", "arguments": {"command": "echo hi"}}
</tool_call>
<tool_call>
{"name": "cat", "arguments": {"file_path": "main.go"}}
</tool_call>
<tool_call>
{"name": "replace", "arguments": {"file_path": "main.go", "old_string": "a", "new_string": "b"}}
</tool_call>
<tool_call>
{"name": "ripgrep", "arguments": {"query": "func main"}}
</tool_call>
<tool_call>
{"name": "find_files", "arguments": {"pattern": "*.go"}}
</tool_call>`

	calls := ParseWebTools(text, defs)
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].Name != "run_command" {
		t.Errorf("expected terminal -> run_command, got %s", calls[0].Name)
	}
	if calls[1].Name != "view_file" {
		t.Errorf("expected cat -> view_file, got %s", calls[1].Name)
	}
	if calls[2].Name != "replace_file_content" {
		t.Errorf("expected replace -> replace_file_content, got %s", calls[2].Name)
	}
	if calls[3].Name != "grep_search" {
		t.Errorf("expected ripgrep -> grep_search, got %s", calls[3].Name)
	}
	if calls[4].Name != "find_by_name" {
		t.Errorf("expected find_files -> find_by_name, got %s", calls[4].Name)
	}
}

func TestCoerceToolArgs_AGYCallMCPTool(t *testing.T) {
	def := types.ToolDef{
		Name: "call_mcp_tool",
		InputSchema: []byte(`{
			"type": "object",
			"required": ["ServerName", "ToolName", "Arguments"],
			"properties": {
				"ServerName": {"type": "string"},
				"ToolName": {"type": "string"},
				"Arguments": {"type": "object"}
			}
		}`),
	}

	input := `{"server_name": "stitch", "tool_name": "list_projects", "arguments": {"limit": 10}}`
	coerced := coerceToolArgs(input, def)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(coerced), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["ServerName"] != "stitch" {
		t.Errorf("expected ServerName='stitch', got %v", parsed["ServerName"])
	}
	if parsed["ToolName"] != "list_projects" {
		t.Errorf("expected ToolName='list_projects', got %v", parsed["ToolName"])
	}
	argsMap, ok := parsed["Arguments"].(map[string]any)
	if !ok || argsMap["limit"] != float64(10) {
		t.Errorf("expected Arguments.limit=10, got %v", parsed["Arguments"])
	}
}

func TestCoerceToolArgs_LineRangesAndPagination(t *testing.T) {
	// 1. Tool expecting StartLine and EndLine
	defLineRange := types.ToolDef{
		Name: "view_file",
		InputSchema: []byte(`{
			"type": "object",
			"properties": {
				"AbsolutePath": {"type": "string"},
				"StartLine": {"type": "integer"},
				"EndLine": {"type": "integer"}
			}
		}`),
	}

	input := `{"file": "test.txt", "offset": 10, "limit": 20}`
	coerced := coerceToolArgs(input, defLineRange)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(coerced), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["StartLine"] != float64(10) {
		t.Errorf("expected StartLine=10, got %v", parsed["StartLine"])
	}
	if parsed["EndLine"] != float64(29) {
		t.Errorf("expected EndLine=29, got %v", parsed["EndLine"])
	}

	// 2. Tool expecting offset and limit
	defOffsetLimit := types.ToolDef{
		Name: "read_file",
		InputSchema: []byte(`{
			"type": "object",
			"properties": {
				"path": {"type": "string"},
				"offset": {"type": "integer"},
				"limit": {"type": "integer"}
			}
		}`),
	}

	input2 := `{"path": "test.txt", "start_line": 5, "end_line": 15}`
	coerced2 := coerceToolArgs(input2, defOffsetLimit)
	var parsed2 map[string]any
	if err := json.Unmarshal([]byte(coerced2), &parsed2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed2["offset"] != float64(5) {
		t.Errorf("expected offset=5, got %v", parsed2["offset"])
	}
	if parsed2["limit"] != float64(11) {
		t.Errorf("expected limit=11, got %v", parsed2["limit"])
	}
}

func TestCoerceToolArgs_ProjectRootResolution(t *testing.T) {
	def := types.ToolDef{
		Name: "view_file",
		InputSchema: []byte(`{
			"type": "object",
			"properties": {
				"AbsolutePath": {"type": "string"}
			}
		}`),
	}

	coerced := coerceToolArgs(`{"path": "src/main.go"}`, def, "/my/workspace")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(coerced), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	expected := "/my/workspace/src/main.go"
	if parsed["AbsolutePath"] != expected {
		t.Errorf("expected AbsolutePath=%q, got %v", expected, parsed["AbsolutePath"])
	}
}

func TestToOpenAIDeltaToolCalls_Indices(t *testing.T) {
	calls := []types.ToolCall{
		{ID: "call_1", Name: "bash", Arguments: `{"command":"ls"}`},
		{ID: "call_2", Name: "read", Arguments: `{"path":"main.go"}`},
	}
	deltas := ToOpenAIDeltaToolCalls(calls)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 delta tool calls, got %d", len(deltas))
	}
	if deltas[0].Index == nil || *deltas[0].Index != 0 {
		t.Errorf("expected deltas[0].Index = 0, got %v", deltas[0].Index)
	}
	if deltas[1].Index == nil || *deltas[1].Index != 1 {
		t.Errorf("expected deltas[1].Index = 1, got %v", deltas[1].Index)
	}

	// Verify JSON marshaling includes index
	b, err := json.Marshal(deltas)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	str := string(b)
	if !strings.Contains(str, `"index":0`) || !strings.Contains(str, `"index":1`) {
		t.Errorf("expected marshaled JSON to contain index:0 and index:1, got: %s", str)
	}
}

func TestParseWebTools_GeminiNativeCall(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "view_file", InputSchema: []byte(`{"properties":{"AbsolutePath":{"type":"string"}}}`)},
	}
	text := `Let me check that file:
call:default_api:view_file{AbsolutePath: "/workspace/main.go", StartLine: 1, EndLine: 50}
`
	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(calls), calls)
	}
	if calls[0].Name != "view_file" {
		t.Errorf("expected view_file, got %s", calls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil || args["AbsolutePath"] != "/workspace/main.go" {
		t.Errorf("expected AbsolutePath key, got %s", calls[0].Arguments)
	}
}

func TestStripInternalThoughtAndToolTags_PreservesMarkdownFences(t *testing.T) {
	input := `<thought>
I should explain how to run tests.
</thought>
Here is how you run tests:

` + "```bash" + `
go test ./...
` + "```" + `

And the JSON config is:
` + "```json" + `
{"key": "value"}
` + "```" + `
`
	got := StripInternalThoughtAndToolTags(input)
	if strings.Contains(got, "<thought>") || strings.Contains(got, "I should explain") {
		t.Errorf("thought was not stripped: %s", got)
	}
	if !strings.Contains(got, "```bash\ngo test ./...\n```") {
		t.Errorf("bash code fence was lost: %s", got)
	}
	if !strings.Contains(got, `{"key": "value"}`) {
		t.Errorf("json code fence was lost: %s", got)
	}
}

func TestCoerceToolArgs_ClampRangeLimits(t *testing.T) {
	def := types.ToolDef{
		Name: "view_file",
		InputSchema: []byte(`{
			"type": "object",
			"properties": {
				"AbsolutePath": {"type": "string"},
				"StartLine": {"type": "integer"},
				"EndLine": {"type": "integer"}
			}
		}`),
	}
	// StartLine = 10, EndLine = 1500 (exceeds max 800 lines limit)
	coerced := coerceToolArgs(`{"AbsolutePath": "/workspace/main.go", "StartLine": 10, "EndLine": 1500}`, def, "/workspace")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(coerced), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	endLine, ok := parsed["EndLine"].(float64)
	if !ok || int(endLine) != 809 {
		t.Errorf("expected EndLine clamped to 809 (10 + 799), got %v", parsed["EndLine"])
	}
}

func TestParseWebTools_PreservesServerNameCasing(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "call_mcp_tool", InputSchema: []byte(`{"properties":{"ServerName":{"type":"string"},"ToolName":{"type":"string"}}}`)},
	}
	text := `<tool_call>
{"name": "call_mcp_tool", "arguments": {"server_name": "StitchMCP", "tool_name": "list_projects"}}
</tool_call>`
	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	serverName, _ := args["ServerName"].(string)
	if serverName != "StitchMCP" {
		t.Errorf("expected ServerName='StitchMCP', got %q", serverName)
	}

	// Test 2: Double-underscore format mapping to call_mcp_tool
	text2 := `<tool_call>
{"name": "mcp__StitchMCP__list_projects", "arguments": {"query": "test"}}
</tool_call>`
	calls2 := ParseWebTools(text2, defs)
	if len(calls2) != 1 || calls2[0].Name != "call_mcp_tool" {
		t.Fatalf("expected 1 call_mcp_tool, got %+v", calls2)
	}
	var args2 map[string]any
	if err := json.Unmarshal([]byte(calls2[0].Arguments), &args2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if args2["ServerName"] != "StitchMCP" || args2["ToolName"] != "list_projects" {
		t.Errorf("expected StitchMCP/list_projects, got %v / %v", args2["ServerName"], args2["ToolName"])
	}

	// Test 3: Without mcp__ prefix: StitchMCP__list_projects mapping to call_mcp_tool
	text3 := `<tool_call>
{"name": "StitchMCP__list_projects", "arguments": {"query": "test"}}
</tool_call>`
	calls3 := ParseWebTools(text3, defs)
	if len(calls3) != 1 || calls3[0].Name != "call_mcp_tool" {
		t.Fatalf("expected 1 call_mcp_tool, got %+v", calls3)
	}
	var args3 map[string]any
	if err := json.Unmarshal([]byte(calls3[0].Arguments), &args3); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if args3["ServerName"] != "StitchMCP" || args3["ToolName"] != "list_projects" {
		t.Errorf("expected StitchMCP/list_projects, got %v / %v", args3["ServerName"], args3["ToolName"])
	}
}

func TestParseWebTools_FunctionCallAndReActFormats(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"properties":{"command":{"type":"string"}}}`)},
	}

	// 1. <function_call> without tag attributes
	xmlBody := `<function_call>
{"name": "Bash", "arguments": {"command": "cargo test"}}
</function_call>`
	calls1 := ParseWebTools(xmlBody, defs)
	if len(calls1) != 1 || calls1[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from <function_call>, got: %+v", calls1)
	}
	if !strings.Contains(calls1[0].Arguments, "cargo test") {
		t.Errorf("expected cargo test argument, got: %s", calls1[0].Arguments)
	}

	// 2. ReAct Action: / Action Input: format
	reactText := `I need to check the build status.
Action: Bash
Action Input: {"command": "go build ./..."}
`
	calls2 := ParseWebTools(reactText, defs)
	if len(calls2) != 1 || calls2[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash call from ReAct format, got: %+v", calls2)
	}
	if !strings.Contains(calls2[0].Arguments, "go build") {
		t.Errorf("expected go build argument, got: %s", calls2[0].Arguments)
	}
}

func TestSplitMCPServerTool(t *testing.T) {
	cases := []struct {
		input      string
		wantServer string
		wantTool   string
	}{
		{"mcp__StitchMCP__list_projects", "StitchMCP", "list_projects"},
		{"mcp_StitchMCP_list_projects", "StitchMCP", "list_projects"},
		{"mcp__supabase-mcp-server__list_tables", "supabase-mcp-server", "list_tables"},
		{"mcp_supabase_mcp_server_list_tables", "supabase-mcp-server", "list_tables"},
		{"mcp__custom_server__tool_name", "custom_server", "tool_name"},
	}
	for _, tc := range cases {
		s, tool := SplitMCPServerTool(tc.input)
		if s != tc.wantServer || tool != tc.wantTool {
			t.Errorf("SplitMCPServerTool(%q) = (%q, %q), want (%q, %q)", tc.input, s, tool, tc.wantServer, tc.wantTool)
		}
	}
}

func TestExtractThoughts(t *testing.T) {
	input := `<thought>
First step is to check database tables.
</thought>
Here is the answer.`
	extracted := ExtractThoughts(input)
	if !strings.Contains(extracted, "First step is to check database tables.") {
		t.Errorf("expected extracted thoughts, got: %q", extracted)
	}
}

func TestBidirectionalMCPMatching(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "list_tables", InputSchema: []byte(`{}`)},
	}
	text := `<tool_call>
{"name": "supabase_mcp_server_list_tables", "arguments": {}}
</tool_call>`
	calls := ParseWebTools(text, defs)
	if len(calls) != 1 || calls[0].Name != "list_tables" {
		t.Fatalf("expected list_tables call, got: %+v", calls)
	}
}

// ChatGPT web otherwise reaches for its own sandbox, finds no repo and
// answers "I have no tools"; both preambles must steer it to <tool_call>.
func TestWebPreambles_SteerAwayFromBuiltinSandbox(t *testing.T) {
	defs := []types.ToolDef{{Name: "Bash"}}
	for name, p := range map[string]string{"preamble": WebPreamble(defs), "reminder": WebCatalogOnly(defs)} {
		if !strings.Contains(p, "python/container") || !strings.Contains(p, "[Tool result]") {
			t.Errorf("%s does not warn off built-in sandbox / explain turn-taking:\n%s", name, p)
		}
	}
}

// Claude Code drops a tool_use whose id already appears earlier in the
// transcript and sends "(no content)" in place of its result, so every web
// tool call must get an id the history has not used yet.
func TestFinalizeWebToolCalls_IDsUniqueAcrossTurns(t *testing.T) {
	defs := []types.ToolDef{{Name: "Bash"}}
	text := `<tool_call>
{"name":"Bash","arguments":{"command":"ls"}}
</tool_call>`
	first, _ := FinalizeWebToolCalls(text, defs, nil)
	if len(first) != 1 || first[0].ID == "" {
		t.Fatalf("first turn: %+v", first)
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "list files"},
		{Role: "assistant", ToolCalls: first},
		{Role: "tool", ToolCallID: first[0].ID, Content: "main.go"},
	}
	second, _ := FinalizeWebToolCalls(text, defs, hist)
	if len(second) != 1 || second[0].ID == "" || second[0].ID == first[0].ID {
		t.Fatalf("second turn reused id %q: %+v", first[0].ID, second)
	}

	// The model may copy an id it saw in the transcript.
	copied := `<tool_call name="Bash" id="` + first[0].ID + `">
{"command":"pwd"}
</tool_call>`
	third, _ := FinalizeWebToolCalls(copied, defs, hist)
	if len(third) != 1 || third[0].ID == first[0].ID {
		t.Fatalf("copied history id kept: %+v", third)
	}
}

func TestIsToolRefusal_VietnameseAndEnglishPhrases(t *testing.T) {
	refusals := []string{
		"Tôi không có quyền truy cập repo của bạn.",
		"Tôi không thể chạy lệnh trực tiếp trên terminal.",
		"mình không thể thực hiện lệnh này được.",
		"Hiện tại không thể thực thi lệnh trong môi trường này.",
		"As an AI, I cannot execute terminal commands.",
		"I am unable to run commands directly on your local machine.",
		"Please run the following command in your terminal:\ngit status",
		"Vui lòng chạy lệnh sau trên terminal của bạn:\ngo test ./...",
	}
	for _, r := range refusals {
		if !IsToolRefusal(r) {
			t.Errorf("expected IsToolRefusal(%q) to be true, got false", r)
		}
	}

	nonRefusals := []string{
		"I will check the git status for you now.",
		"<tool_call>{\"name\":\"Bash\",\"arguments\":{\"command\":\"git status\"}}</tool_call>",
		"Đang kiểm tra trạng thái của kho lưu trữ.",
	}
	for _, nr := range nonRefusals {
		if IsToolRefusal(nr) {
			t.Errorf("expected IsToolRefusal(%q) to be false, got true", nr)
		}
	}
}

func TestPyKwargsToJSON(t *testing.T) {
	cases := []struct {
		input    string
		expected map[string]any
	}{
		{
			input: `(command="git status", timeout=30)`,
			expected: map[string]any{
				"command": "git status",
				"timeout": int64(30),
			},
		},
		{
			input: `(AbsolutePath="/Users/foo/bar.go", StartLine=1, EndLine=50, Force=True)`,
			expected: map[string]any{
				"AbsolutePath": "/Users/foo/bar.go",
				"StartLine":    int64(1),
				"EndLine":      int64(50),
				"Force":        true,
			},
		},
		{
			input:    `()`,
			expected: map[string]any{},
		},
	}

	for _, c := range cases {
		jsonStr, ok := pyKwargsToJSON(c.input)
		if !ok {
			t.Fatalf("pyKwargsToJSON(%q) failed", c.input)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Fatalf("failed to unmarshal result %s: %v", jsonStr, err)
		}
		for k, v := range c.expected {
			if parsed[k] != v {
				// Special check for number representations
				if intV, isInt := v.(int64); isInt {
					if floatV, isFloat := parsed[k].(float64); isFloat && int64(floatV) == intV {
						continue
					}
				}
				t.Errorf("key %q: expected %v (%T), got %v (%T)", k, v, v, parsed[k], parsed[k])
			}
		}
	}
}

func TestParseWebTools_GeminiCallKwargs(t *testing.T) {
	defs := []types.ToolDef{
		{
			Name:        "view_file",
			InputSchema: []byte(`{"properties":{"AbsolutePath":{"type":"string"}},"required":["AbsolutePath"]}`),
		},
	}
	text := `Let me inspect the file:
call:default_api:view_file(AbsolutePath="/Users/test/main.go")
`
	calls := ParseWebTools(text, defs)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(calls), calls)
	}
	if calls[0].Name != "view_file" {
		t.Errorf("expected view_file, got %s", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "/Users/test/main.go") {
		t.Errorf("arguments missing path: %s", calls[0].Arguments)
	}
}

func TestStreamThoughtExtractor_ReflectionTag(t *testing.T) {
	e := &streamThoughtExtractor{}
	th, co := e.Feed("<reflection>\nAnalyzing user request...\n</reflection>\nHere is the answer.")
	if !strings.Contains(th, "Analyzing user request") {
		t.Errorf("expected thought to contain 'Analyzing user request', got: %q", th)
	}
	if !strings.Contains(co, "Here is the answer.") {
		t.Errorf("expected content to contain 'Here is the answer.', got: %q", co)
	}
}
