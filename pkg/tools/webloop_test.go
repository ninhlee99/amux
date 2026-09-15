package tools

import (
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

func TestIsWebToolRefusal(t *testing.T) {
	if !isWebToolRefusal("Chưa đọc được repo, không mount thư mục. Gửi cho tôi README.") {
		t.Fatal("vn refusal")
	}
	if !isWebToolRefusal("I cannot access the files. Please paste README.md") {
		t.Fatal("en refusal")
	}
	if isWebToolRefusal(`{"title":"README dự án"}`) {
		t.Fatal("title is not refusal")
	}
}

func TestWrapWebStream_RefusalBecomesToolUse(t *testing.T) {
	inner := make(chan types.StreamChunk, 2)
	inner <- types.StreamChunk{Content: "Chưa đọc được repo, không mount. Gửi cho tôi README."}
	close(inner)
	out := wrapWebStream("chatgpt:01", []types.ToolDef{{Name: "Read"}, {Name: "Bash"}}, nil, inner)
	var calls []types.ToolCall
	var content string
	for ch := range out {
		content += ch.Content
		if len(ch.ToolCalls) > 0 {
			calls = ch.ToolCalls
		}
	}
	if len(calls) < 1 || len(calls) > maxForcedWebTools {
		t.Fatalf("calls=%+v", calls)
	}
	// No path with extension → bash git status only (never invent README.md).
	if calls[0].Name != "Bash" || !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("want safe bash explore, got %+v", calls)
	}
	if strings.Contains(content, "Gửi cho tôi") {
		t.Fatal("refusal leaked")
	}
}

func TestFallbackExploreTools_UsesCatalogKeys(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Read", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}
	calls := fallbackExploreTools(defs, nil)
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("no path → bash only: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "git status") || strings.Contains(calls[0].Arguments, "README.md") {
		t.Fatal(calls[0].Arguments)
	}
	withPath := fallbackExploreTools(defs, []types.ChatMessage{
		{Role: "user", Content: "review pkg/cli/cli.go please"},
	})
	if len(withPath) != 1 || withPath[0].Name != "Read" || !strings.Contains(withPath[0].Arguments, "cli.go") {
		t.Fatalf("path from user: %+v", withPath)
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

func TestReGitDiffAndStatusMatching(t *testing.T) {
	validDiffs := []string{
		"git diff",
		"git   diff",
		"/usr/bin/git diff",
		"run `git diff` to check",
		"\"git diff\"",
		"'git diff'",
		// bypass variants that should now match
		"git --no-pager diff",
		"git -C /some/path diff",
		"env git diff",
		"command git diff",
		"/usr/bin/git --no-pager diff",
		"/usr/local/bin/git diff",
		"/opt/homebrew/bin/git diff",
		"./git diff",
		"git -C \"path with spaces\" diff",
		"git -C 'path with spaces' diff",
		"git --git-dir=\"/repo/.git\" diff",
		"git --no-pager -C /repo diff",
	}
	for _, s := range validDiffs {
		if !reGitDiffCmd.MatchString(s) {
			t.Errorf("expected %q to match reGitDiffCmd", s)
		}
	}

	invalidDiffs := []string{
		"git diffsomething",
		"diff",
		"difference",
		"indifferent",
		"git_diff",
	}
	for _, s := range invalidDiffs {
		if reGitDiffCmd.MatchString(s) {
			t.Errorf("expected %q NOT to match reGitDiffCmd", s)
		}
	}

	validStatuses := []string{
		"git status",
		"git   status",
		"/usr/bin/git status",
		"/usr/local/bin/git status",
		"/opt/homebrew/bin/git status",
		"./git status",
		"`git status`",
		// bypass variants
		"git --no-pager status",
		"git -C /repo status",
		"git -C \"path with spaces\" status",
		"git -C 'path with spaces' status",
		"env git status",
		"command git status",
	}
	for _, s := range validStatuses {
		if !reGitStatusCmd.MatchString(s) {
			t.Errorf("expected %q to match reGitStatusCmd", s)
		}
	}

	invalidStatuses := []string{
		"status",
		"git statusupdate",
		"git_status",
	}
	for _, s := range invalidStatuses {
		if reGitStatusCmd.MatchString(s) {
			t.Errorf("expected %q NOT to match reGitStatusCmd", s)
		}
	}
}
