package tools

import (
	"encoding/json"
	"testing"

	"amux-accounts/pkg/types"
)

// fuzzDefs is a Claude Code–like catalog: built-ins plus one MCP tool.
var fuzzDefs = []types.ToolDef{
	{Name: "Bash", InputSchema: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"},"timeout":{"type":"number"}}}`)},
	{Name: "Read", InputSchema: json.RawMessage(`{"type":"object","required":["file_path"],"properties":{"file_path":{"type":"string"},"offset":{"type":"number"},"limit":{"type":"number"}}}`)},
	{Name: "Edit", InputSchema: json.RawMessage(`{"type":"object","required":["file_path","old_string","new_string"],"properties":{"file_path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}}}`)},
	{Name: "Grep", InputSchema: json.RawMessage(`{"type":"object","required":["pattern"],"properties":{"pattern":{"type":"string"},"path":{"type":"string"}}}`)},
	{Name: "mcp__github__create_issue", InputSchema: json.RawMessage(`{"type":"object","required":["title"],"properties":{"title":{"type":"string"},"body":{"type":"string"}}}`)},
}

// FuzzParseWebTools checks the invariants every web reply must keep, whatever
// markup the model invents: no panic, only catalog tool names, and arguments
// that are a JSON object the IDE can execute.
func FuzzParseWebTools(f *testing.F) {
	seeds := []string{
		"<<<AMUX_TOOL name=\"Read\" id=\"toolu_web_1\">>>\n{\"file_path\":\"README.md\"}\n<<<END_AMUX_TOOL>>>",
		"<tool_call>\n{\"name\": \"Bash\", \"arguments\": {\"command\": \"git status\"}}\n</tool_call>",
		"run:\n```bash\ngit diff -- README.md\n```\n",
		"<tool_call name=\"Grep\" pattern=\"TODO\" path=\"pkg\"/>",
		"Action: Bash\nAction Input: {\"command\": \"ls\"}",
		"```json\n{\"function_call\": {\"name\": \"Read\", \"arguments\": \"{\\\"file_path\\\":\\\"a.go\\\"}\"}}\n```",
		"<tool_call>{\"name\":\"Bash\",\"arguments\":{\"command\":\"echo \"hi\"\"}}</tool_call>",
		"<<<AMUX_TOOL name=\"mcp__github__create_issue\">>>\n{\"title\":\"x\"}\n<<<END_AMUX_TOOL>>>",
		"<<<AMUX_TOOL name=\"Read\">>>\n{\"file_path\":",
		"<tool_call>\n{\"name\": \"Unknown\", \"arguments\": {}}\n</tool_call>",
		"I'll run:\n<function_calls><invoke name=\"Bash\"><parameter name=\"command\">pwd</parameter></invoke></function_calls>",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	allowed := map[string]bool{}
	for _, d := range fuzzDefs {
		allowed[d.Name] = true
	}
	f.Fuzz(func(t *testing.T, text string) {
		calls := ParseWebTools(text, fuzzDefs)
		for _, c := range calls {
			if !allowed[c.Name] {
				t.Fatalf("tool %q is not in the catalog (input %q)", c.Name, text)
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(c.Arguments), &obj); err != nil {
				t.Fatalf("tool %s arguments not a JSON object: %q (input %q)", c.Name, c.Arguments, text)
			}
		}
		stripped := StripWebToolMarkup(text)
		if reAMUXTool.MatchString(stripped) {
			t.Fatalf("complete AMUX tool block survived StripWebToolMarkup: %q", stripped)
		}
		_ = StripInternalThoughtAndToolTags(text)
		_, _ = FinalizeWebToolCalls(text, fuzzDefs, nil)
	})
}

// ReAct replies with a bare value ("Input: ls") used to reach the IDE as a
// non-object tool input, which Claude Code rejects.
func TestParseWebTools_BareReActInputBecomesObject(t *testing.T) {
	calls := ParseWebTools("Action: Bash\nInput: ls -la\n", fuzzDefs)
	if len(calls) != 1 || calls[0].Name != "Bash" || calls[0].Arguments != `{"command":"ls -la"}` {
		t.Fatalf("bare input: %+v", calls)
	}
	if calls := ParseWebTools("Action: sh\nInput: \"\"\n", fuzzDefs); len(calls) != 0 {
		t.Fatalf("empty command must not become a call: %+v", calls)
	}
	if calls := ParseWebTools("Action: Edit\nInput: main.go\n", fuzzDefs); len(calls) != 0 {
		t.Fatalf("multi-param tool cannot take a bare value: %+v", calls)
	}
}
