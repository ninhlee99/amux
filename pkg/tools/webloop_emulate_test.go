package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "am-tools-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("AM_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func claudeCatalog() []types.ToolDef {
	return []types.ToolDef{
		{Name: "Read", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
		{Name: "Skill", InputSchema: []byte(`{"required":["skill"],"properties":{"skill":{"type":"string"}}}`)},
		{Name: "mcp__github__list_prs", InputSchema: []byte(`{"required":["repo"],"properties":{"repo":{"type":"string"}}}`)},
	}
}

func collectStream(ch <-chan types.StreamChunk) (content string, calls []types.ToolCall, reason string) {
	for c := range ch {
		content += c.Content
		if len(c.ToolCalls) > 0 {
			calls = c.ToolCalls
		}
		if c.FinishReason != "" {
			reason = c.FinishReason
		}
	}
	return
}

func TestParseWebTools_ZhaeesXML_Table(t *testing.T) {
	defs := claudeCatalog()
	tests := []struct {
		name    string
		text    string
		wantN   int
		want0   string
		wantArg string
	}{
		{
			name:    "single read",
			text:    "<tool_call>\n{\"name\":\"Read\",\"arguments\":{\"file_path\":\"README.md\"}}\n</tool_call>",
			wantN:   1,
			want0:   "Read",
			wantArg: "README.md",
		},
		{
			name: "multi read+bash",
			text: `<tool_call>
{"name": "Read", "arguments": {"file_path": "README.md"}}
</tool_call>
<tool_call>
{"name": "Bash", "arguments": {"command": "git diff --stat"}}
</tool_call>`,
			wantN:   2,
			want0:   "Read",
			wantArg: "README.md",
		},
		{
			name:    "trailing comma",
			text:    `<tool_call>{"name":"Bash","arguments":{"command":"ls",},}</tool_call>`,
			wantN:   1,
			want0:   "Bash",
			wantArg: "ls",
		},
		{
			name:    "skill",
			text:    `<tool_call>{"name":"Skill","arguments":{"skill":"caveman"}}</tool_call>`,
			wantN:   1,
			want0:   "Skill",
			wantArg: "caveman",
		},
		{
			name:    "mcp live catalog",
			text:    `<tool_call>{"name":"mcp__github__list_prs","arguments":{"repo":"ninhlee99/amux"}}</tool_call>`,
			wantN:   1,
			want0:   "mcp__github__list_prs",
			wantArg: "ninhlee99/amux",
		},
		{
			name:  "unknown tool dropped",
			text:  `<tool_call>{"name":"NukeDisk","arguments":{}}</tool_call>`,
			wantN: 0,
		},
		{
			name:  "prose only",
			text:  "I will review README when you paste it.",
			wantN: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseWebTools(tc.text, defs)
			if len(got) != tc.wantN {
				t.Fatalf("n=%d want %d got=%+v", len(got), tc.wantN, got)
			}
			if tc.wantN == 0 {
				return
			}
			if got[0].Name != tc.want0 {
				t.Fatalf("name=%s want %s", got[0].Name, tc.want0)
			}
			if tc.wantArg != "" && !strings.Contains(got[0].Arguments, tc.wantArg) {
				t.Fatalf("args=%s want %q", got[0].Arguments, tc.wantArg)
			}
		})
	}
}

func TestParseToolCallJSON_ArgumentsObjectAndEmpty(t *testing.T) {
	name, _, args, ok := parseToolCallJSON(`{"name":"Read","arguments":{"file_path":"a.go"}}`)
	if !ok || name != "Read" || !strings.Contains(args, "a.go") {
		t.Fatalf("%s %s %v", name, args, ok)
	}
	name, _, args, ok = parseToolCallJSON(`{"name":"Read","input":{"path":"b.go"}}`)
	if !ok || name != "Read" || !strings.Contains(args, "b.go") {
		t.Fatalf("input alias: %s %s %v", name, args, ok)
	}
	name, _, args, ok = parseToolCallJSON(`{"name":"Bash"}`)
	if !ok || name != "Bash" || args != "{}" {
		t.Fatalf("%s %s %v", name, args, ok)
	}
	if _, _, _, ok = parseToolCallJSON(`not-json`); ok {
		t.Fatal("bad json")
	}
}

func TestWrapWebStream_XMLBecomesAPIKeyStyleToolUse(t *testing.T) {
	inner := make(chan types.StreamChunk, 4)
	inner <- types.StreamChunk{Content: "ok\n"}
	inner <- types.StreamChunk{Content: `<tool_call>
{"name":"Read","arguments":{"file_path":"README.md"}}
</tool_call>
<tool_call>
{"name":"Bash","arguments":{"command":"git diff"}}
</tool_call>`}
	close(inner)

	content, calls, reason := collectStream(wrapWebStream("chatgpt:01", claudeCatalog(), nil, inner))
	if reason != "tool_calls" {
		t.Fatalf("reason=%q", reason)
	}
	if len(calls) != 2 || calls[0].Name != "Read" || calls[1].Name != "Bash" {
		t.Fatalf("calls=%+v", calls)
	}
	if strings.Contains(content, "<tool_call>") || strings.Contains(content, "file_path") {
		t.Fatalf("markup leaked to client: %q", content)
	}
}

func TestWrapWebStream_ChunkedXML(t *testing.T) {
	inner := make(chan types.StreamChunk, 8)
	parts := []string{"<tool_", `call>{"name":`, `"Read","argum`, `ents":{"file_path":"STRUCT.md"}}`, `</tool_call>`}
	for _, p := range parts {
		inner <- types.StreamChunk{Content: p}
	}
	close(inner)
	_, calls, reason := collectStream(wrapWebStream("gemini:web:01", claudeCatalog(), nil, inner))
	if reason != "tool_calls" || len(calls) != 1 || calls[0].Name != "Read" {
		t.Fatalf("reason=%s calls=%+v", reason, calls)
	}
	if !strings.Contains(calls[0].Arguments, "STRUCT.md") {
		t.Fatal(calls[0].Arguments)
	}
}

func TestMaybeWrapWebStream_NoToolsPassthrough(t *testing.T) {
	inner := make(chan types.StreamChunk, 1)
	inner <- types.StreamChunk{Content: "hello", Done: true}
	close(inner)
	out := MaybeWrapWebStream("chatgpt:01", &types.ChatRequest{}, inner)
	if out != inner {
		t.Fatal("no tools[] must skip wrap (API-key path)")
	}
}

func TestMaybeWrapWebStream_TitleJSONNotForced(t *testing.T) {
	inner := make(chan types.StreamChunk, 1)
	inner <- types.StreamChunk{Content: `{"title":"README dự án"}`}
	close(inner)
	content, calls, _ := collectStream(MaybeWrapWebStream("chatgpt:01", &types.ChatRequest{
		Tools: claudeCatalog(),
	}, inner))
	if len(calls) != 0 {
		t.Fatalf("title must not become tools: %+v", calls)
	}
	if !strings.Contains(content, "README dự án") {
		t.Fatal(content)
	}
}

func TestParseWebTools_BracketToolCall_LiveLog(t *testing.T) {
	// ChatGPT actually emitted this (amux.log 20:16:53), not XML.
	text := `
Chưa đọc được repo.
[tool_call name=Bash id=toolu_web_1]
{"command":"git diff -- README.md\ngit diff --stat\ngit diff"}
[tool_call name=Read id=toolu_web_2]
{"file_path":"README.md"}
`
	got := ParseWebTools(text, claudeCatalog())
	if len(got) != 2 || got[0].Name != "Bash" || got[1].Name != "Read" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[0].Arguments, "git diff") || !strings.Contains(got[1].Arguments, "README.md") {
		t.Fatalf("args %+v", got)
	}
}

func TestParseWebTools_BracketHistoryVariations(t *testing.T) {
	text := `
Let's execute the next steps:
[Tool call: Bash id=toolu_web_1]
{"command":"ls -la"}
[Tool call: Read]
{"file_path":"main.go"}
[tool_call: Bash]
{"command":"go test ./..."}
`
	got := ParseWebTools(text, claudeCatalog())
	if len(got) != 3 {
		t.Fatalf("expected 3 calls, got %d: %+v", len(got), got)
	}
	if got[0].Name != "Bash" || !strings.Contains(got[0].Arguments, "ls -la") {
		t.Errorf("call 0 mismatch: %+v", got[0])
	}
	if got[1].Name != "Read" || !strings.Contains(got[1].Arguments, "main.go") {
		t.Errorf("call 1 mismatch: %+v", got[1])
	}
	if got[2].Name != "Bash" || !strings.Contains(got[2].Arguments, "go test") {
		t.Errorf("call 2 mismatch: %+v", got[2])
	}

	stripped := StripWebToolMarkup(text)
	if strings.Contains(stripped, "[Tool call:") || strings.Contains(stripped, "[tool_call:") {
		t.Errorf("expected bracket markup stripped, got: %q", stripped)
	}
}

func TestWrapWebStream_LiveRefusalPassesThroughCleanly(t *testing.T) {
	text := "Không đọc được repo từ môi trường tool hiện tại. Tool shell đang chạy container khác, không thấy `/Users/ninh.le/Documents/apps/amux`, nên chưa thể review README/code thay đổi thật."
	inner := make(chan types.StreamChunk, 1)
	inner <- types.StreamChunk{Content: text}
	close(inner)

	content, calls, reason := collectStream(wrapWebStream("chatgpt:01", claudeCatalog(), nil, inner))
	if len(calls) != 0 {
		t.Fatalf("expected 0 forced tool calls, got: %+v", calls)
	}
	if reason == "tool_calls" {
		t.Fatalf("expected plain completion, got reason %q", reason)
	}
	if !strings.Contains(content, "Không đọc được repo") {
		t.Fatalf("expected original text pass-through, got: %q", content)
	}
}

func TestWrapWebStream_ExplanatoryProsePassesThroughCleanly(t *testing.T) {
	liveText := `Kết quả hiện có cho thấy:
- README.md có 151 dòng thay đổi.
- Code thêm các thành phần mới:
  - Gemini bridge (pkg/bridge/gemini.go)
  - Account CLI`

	inner := make(chan types.StreamChunk, 1)
	inner <- types.StreamChunk{Content: liveText}
	close(inner)

	content, calls, _ := collectStream(wrapWebStream("chatgpt:01", claudeCatalog(), nil, inner))
	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls on explanatory prose, got %+v", calls)
	}
	if !strings.Contains(content, "Gemini bridge") {
		t.Fatalf("expected content to be preserved, got: %q", content)
	}
}

func TestCoerceToolArgs_InstructionAndDescription(t *testing.T) {
	def := types.ToolDef{
		Name: "replace_file_content",
		InputSchema: json.RawMessage(`{
			"type": "OBJECT",
			"properties": {
				"TargetFile": {"type": "STRING"},
				"TargetContent": {"type": "STRING"},
				"ReplacementContent": {"type": "STRING"},
				"Instruction": {"type": "STRING"},
				"Description": {"type": "STRING"},
				"toolAction": {"type": "STRING"},
				"toolSummary": {"type": "STRING"}
			},
			"required": ["TargetFile", "Instruction", "Description", "toolAction", "toolSummary"]
		}`),
	}

	incomingArgs := `{"TargetFile":"main.go","TargetContent":"a","ReplacementContent":"b"}`
	coerced := coerceToolArgs(incomingArgs, def)

	var m map[string]any
	if err := json.Unmarshal([]byte(coerced), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["Instruction"] != "Apply modifications" {
		t.Errorf("expected default Instruction 'Apply modifications', got %v", m["Instruction"])
	}
	if m["Description"] != "Code change" {
		t.Errorf("expected default Description 'Code change', got %v", m["Description"])
	}
	if m["toolAction"] != "Running tool" {
		t.Errorf("expected default toolAction 'Running tool', got %v", m["toolAction"])
	}
	if m["toolSummary"] != "Tool execution" {
		t.Errorf("expected default toolSummary 'Tool execution', got %v", m["toolSummary"])
	}
}

