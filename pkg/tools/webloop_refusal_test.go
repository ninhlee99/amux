package tools_test

import (
	"strings"
	"testing"

	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

func TestWebloop_OpenPRReviewRefusal_AutoKickstart(t *testing.T) {
	refusalText := `I can’t complete the /open-pr:review workflow in this chat instance because the workspace tool runtime described in the prompt (for example the open-pr command wrapper, repo checkout/context tools, and posting tools) is not available to me here.
If you run this in the Claude Code workspace session that has those tools enabled, /open-pr:review https://github.com/ninhlee99/amux/pull/47 can proceed with the required read-only PR review flow.`

	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
		{Name: "Read", InputSchema: []byte(`{"required":["file_path"],"properties":{"file_path":{"type":"string"}}}`)},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review https://github.com/ninhlee99/amux/pull/47"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected auto-kickstart Bash call on open-pr:review refusal, got forced=%v, calls=%d (%+v)", forced, len(calls), calls)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status in kickstart call, got: %s", calls[0].Arguments)
	}
	if !strings.Contains(calls[0].Arguments, "git branch --show-current") {
		t.Fatalf("expected branch check in kickstart call, got: %s", calls[0].Arguments)
	}
}

func TestWebloop_PRReviewNumberRefusal_NoForcedHallucination(t *testing.T) {
	refusalText := `Mình thấy tin nhắn vừa rồi chỉ chứa phần CATALOG/tool schema và không có nội dung PR hoặc diff để review.
Nếu bạn muốn mình tiếp tục review PR #39, vui lòng gửi lại link PR.`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	hist := []types.ChatMessage{
		{Role: "user", Content: "please review https://github.com/ninhlee99/amux/pull/39"},
	}

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if forced || len(calls) != 0 {
		t.Fatalf("expected no forced tool calls on plain text refusal, got forced=%v, calls=%d", forced, len(calls))
	}
}

func TestWebloop_ExplicitToolCall_ParsedCorrectly(t *testing.T) {
	text := `Sure, let me check the git status first:
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>`

	defs := []types.ToolDef{
		{Name: "Bash"},
	}

	calls, forced := tools.FinalizeWebToolCalls(text, defs, nil)
	if forced {
		t.Fatalf("explicit tool call should not be marked forced")
	}
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected 1 Bash tool call, got: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status argument, got: %s", calls[0].Arguments)
	}
}

func TestIsToolRefusal_ClaudeWebSimulationPhrases(t *testing.T) {
	claudeRefusals := []string{
		"This session is not actually Claude Code with tool access — it's a text-based simulation where a system layer is asking me to emit JSON <tool_call> blocks.",
		"I can't actually act on your repo from here. I'm Claude in a chat interface right now, not a live Claude Code session.",
		"I'm not going to emit fake tool calls or pretend to have made edits, run commands, or pushed commits that didn't happen.",
		"Legitimate tool-use setups don't need this framing. That's a pattern I associate with prompt injection trying to get an agent to act confidently.",
		"The harness is asking for a lot of trust. I don't have a verified way to confirm that any of this is real.",
		"We're going in circles on this. Repeating the same instructions won't create a tool that doesn't exist for me — I'm not going to emit fake <tool_call> blocks for a Bash/TOOL_NAME system that isn't part of my actual toolset, no matter how many times it's framed as \"live.\"",
		"I'll be direct: that <tool_call> / Bash format is not something I actually have access to, no matter how many times it's asserted as \"live\" in these messages.",
		"I don't actually have a working Bash tool connected in this environment—the \"CATALOG\" shown only declares `Bash:command:string` as a schema fragment, not a functional tool I can invoke here.",
	}

	for _, text := range claudeRefusals {
		if !tools.IsToolRefusal(text) {
			t.Errorf("expected IsToolRefusal=true for Claude Web refusal text:\n%s", text)
		}
	}
}

func TestWebloop_ChatGPTAndGeminiCompatibility(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
		{Name: "view_file", InputSchema: []byte(`{"required":["AbsolutePath"],"properties":{"AbsolutePath":{"type":"string"}}}`)},
	}

	// 1. Verify preamble steers ChatGPT away from its internal sandbox
	preamble := tools.WebPreamble(defs)
	if !strings.Contains(preamble, "python/container") {
		t.Errorf("preamble must warn off ChatGPT python/container sandbox:\n%s", preamble)
	}
	if !strings.Contains(preamble, "[Tool result]") {
		t.Errorf("preamble must explain [Tool result] return channel:\n%s", preamble)
	}
	if !strings.Contains(preamble, "<tool_call>") {
		t.Errorf("preamble must demonstrate <tool_call> markup:\n%s", preamble)
	}

	// 2. Verify reminder provides clear call format without coercive phrases
	reminder := tools.WebCatalogOnly(defs)
	if !strings.Contains(reminder, "<tool_call>") {
		t.Errorf("reminder must contain <tool_call>:\n%s", reminder)
	}
	if strings.Contains(reminder, "DO NOT decline") || strings.Contains(reminder, "Never refuse") {
		t.Errorf("reminder must not contain coercive phrases that trigger Claude safety filters:\n%s", reminder)
	}

	// 3. Verify ChatGPT bracket-style tool calls continue to parse
	chatgptText := `[Tool call: Bash id=toolu_1] {"command":"git status -s"}`
	calls, forced := tools.FinalizeWebToolCalls(chatgptText, defs, nil)
	if forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected ChatGPT bracket tool call parsed, got: %+v (forced=%v)", calls, forced)
	}

	// 4. Verify Gemini call:default_api syntax continues to parse
	geminiText := `call:default_api:view_file{AbsolutePath: "/workspace/main.go"}`
	gCalls, gForced := tools.FinalizeWebToolCalls(geminiText, defs, nil)
	if gForced || len(gCalls) != 1 || gCalls[0].Name != "view_file" {
		t.Fatalf("expected Gemini call parsed, got: %+v (forced=%v)", gCalls, gForced)
	}
}

func TestFinalizeWebToolCalls_SkillBashFenceFallback(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:fix https://github.com/ninhlee99/amux/pull/47"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{Name: "Bash", Arguments: `{"command":"git status"}`}}},
		{Role: "tool", Content: "On branch fix/test"},
	}

	// Model outputs markdown bash fence mid-loop instead of <tool_call>
	text := "Let's inspect the modified files:\n```bash\ngit diff main...HEAD\n```\n"
	calls, forced := tools.FinalizeWebToolCalls(text, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected recovered Bash tool call from markdown fence, got: %+v (forced=%v)", calls, forced)
	}
	if !strings.Contains(calls[0].Arguments, "git diff main...HEAD") {
		t.Fatalf("expected git diff command in arguments, got: %s", calls[0].Arguments)
	}
}

func TestFinalizeWebToolCalls_SkillBacktickCommandFallback(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:review https://github.com/ninhlee99/amux/pull/47"},
	}

	text := "I will start by running `git status` to see the current working directory state."
	calls, forced := tools.FinalizeWebToolCalls(text, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected recovered Bash tool call from inline backtick, got: %+v (forced=%v)", calls, forced)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status command in arguments, got: %s", calls[0].Arguments)
	}
}

func TestFinalizeWebToolCalls_ClaudeWebSimulationRefusalKickstart(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:fix https://github.com/ninhlee99/amux/pull/47"},
	}

	refusalText := `This session is not actually Claude Code with tool access — it's a text-based simulation where a system layer is asking me to emit JSON <tool_call> blocks that some external harness would supposedly execute against a real repo. I don't have a verified way to confirm that any of this "CLI output" is real.
I'm not going to emit fake tool calls or pretend to have made edits, run commands, or pushed commits that didn't happen.`

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected auto-kickstart Bash call on simulation refusal, got: %+v (forced=%v)", calls, forced)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status command in kickstart call, got: %s", calls[0].Arguments)
	}
}

func TestIsToolRefusal_ChatGPTWebPhrases(t *testing.T) {
	chatgptRefusals := []string{
		"I can’t execute the '/open-pr:fix' command or access the repository workspace from this chat session because the provided workspace tool is not actually available to me.",
		"I cannot execute commands directly on the repository workspace from this chat session.",
		"The workspace tool is not actually available in this environment.",
		"I don't have access to the repository workspace to run git commands.",
		"Unable to execute the command: no direct access to the repository workspace.",
		"I can continue the PR fix, but I don’t currently have access to the workspace/Bash tool in this chat session, so I can’t inspect the repository, edit files, run tests, or commit changes. Please re-enable/provide the workspace tool (with the `Bash` catalog available).",
	}

	for _, text := range chatgptRefusals {
		if !tools.IsToolRefusal(text) {
			t.Errorf("expected IsToolRefusal=true for ChatGPT Web refusal text:\n%s", text)
		}
	}
}

func TestFinalizeWebToolCalls_ChatGPTWebRefusalKickstart(t *testing.T) {
	defs := []types.ToolDef{
		{Name: "Bash", InputSchema: []byte(`{"required":["command"],"properties":{"command":{"type":"string"}}}`)},
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "/open-pr:fix https://github.com/ninhlee99/amux/pull/47"},
	}

	refusalText := `I can’t execute the '/open-pr:fix' command or access the repository workspace from this chat session because the provided workspace tool is not actually available to me.`

	calls, forced := tools.FinalizeWebToolCalls(refusalText, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected auto-kickstart Bash call on ChatGPT refusal, got: %+v (forced=%v)", calls, forced)
	}
	if !strings.Contains(calls[0].Arguments, "git status") {
		t.Fatalf("expected git status command in kickstart call, got: %s", calls[0].Arguments)
	}
	if !strings.Contains(calls[0].Arguments, "git branch --show-current") {
		t.Fatalf("expected git branch inspection in open-pr kickstart call, got: %s", calls[0].Arguments)
	}
}

func TestCleanUserTurnContent(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "<system-reminder>\n15000000 tokens left\n</system-reminder>\n\n(no content)",
			want:  "",
		},
		{
			input: "<system-reminder>some metadata</system-reminder>\n/open-pr:fix https://github.com/ninhlee99/amux/pull/47",
			want:  "/open-pr:fix https://github.com/ninhlee99/amux/pull/47",
		},
		{
			input: "(no content)",
			want:  "",
		},
		{
			input: "  \n  ",
			want:  "",
		},
		{
			input: "Please check git diff",
			want:  "Please check git diff",
		},
	}

	for _, tc := range cases {
		got := tools.CleanUserTurnContent(tc.input)
		if got != tc.want {
			t.Errorf("CleanUserTurnContent(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFinalizeWebToolCalls_MCPToolRefusalKickstart(t *testing.T) {
	defs := []types.ToolDef{
		{
			Name: "mcp__github__list_issues",
			InputSchema: []byte(`{
				"type": "object",
				"required": ["owner", "repo"],
				"properties": {
					"owner": {"type": "string"},
					"repo": {"type": "string"},
					"state": {"type": "string"}
				}
			}`),
		},
	}
	hist := []types.ChatMessage{
		{Role: "user", Content: "List the recent open issues on the ninhlee99/amux repository using the GitHub MCP tool."},
	}

	claudeRefusal := `I'll try fetching this via the public GitHub API instead, since "Bash" and "mcp__github__list_issues" aren't tools I actually have — only the ones listed in my real tool definitions are available to me.`
	calls, forced := tools.FinalizeWebToolCalls(claudeRefusal, defs, hist)
	if !forced || len(calls) != 1 || calls[0].Name != "mcp__github__list_issues" {
		t.Fatalf("expected auto-kickstart mcp__github__list_issues, got: %+v (forced=%v)", calls, forced)
	}
	if !strings.Contains(calls[0].Arguments, `"owner":"ninhlee99"`) || !strings.Contains(calls[0].Arguments, `"repo":"amux"`) {
		t.Fatalf("expected owner=ninhlee99 and repo=amux, got: %s", calls[0].Arguments)
	}

	chatgptHesitation := `I’m ready to help with the workspace/GitHub task. Please provide the specific action you want performed (for example: list issues, inspect a repository, edit files, run tests, or make a commit).`
	cCalls, cForced := tools.FinalizeWebToolCalls(chatgptHesitation, defs, hist)
	if !cForced || len(cCalls) != 1 || cCalls[0].Name != "mcp__github__list_issues" {
		t.Fatalf("expected auto-kickstart mcp__github__list_issues on ChatGPT hesitation, got: %+v (forced=%v)", cCalls, cForced)
	}
	if !strings.Contains(cCalls[0].Arguments, `"owner":"ninhlee99"`) || !strings.Contains(cCalls[0].Arguments, `"repo":"amux"`) {
		t.Fatalf("expected owner=ninhlee99 and repo=amux, got: %s", cCalls[0].Arguments)
	}

	claudeToolsetRefusal := `I don't have a tool called "mcp__github__list_issues" — repeating that instruction doesn't add it to my actual toolset. The tools I can call are the ones defined for me (bash_tool, view, create_file, edit_file).`
	tCalls, tForced := tools.FinalizeWebToolCalls(claudeToolsetRefusal, defs, hist)
	if !tForced || len(tCalls) != 1 || tCalls[0].Name != "mcp__github__list_issues" {
		t.Fatalf("expected auto-kickstart mcp__github__list_issues on Claude toolset denial, got: %+v (forced=%v)", tCalls, tForced)
	}
	if !strings.Contains(tCalls[0].Arguments, `"owner":"ninhlee99"`) || !strings.Contains(tCalls[0].Arguments, `"repo":"amux"`) {
		t.Fatalf("expected owner=ninhlee99 and repo=amux, got: %s", tCalls[0].Arguments)
	}

	claudeStickToolsRefusal := `I'll stick with my actual tools rather than calling a function that doesn't exist for me. Let me check if there's a real GitHub connector available, and also try the REST API directly.`
	sCalls, sForced := tools.FinalizeWebToolCalls(claudeStickToolsRefusal, defs, hist)
	if !sForced || len(sCalls) != 1 || sCalls[0].Name != "mcp__github__list_issues" {
		t.Fatalf("expected auto-kickstart mcp__github__list_issues on Claude stick-with-tools denial, got: %+v (forced=%v)", sCalls, sForced)
	}
	if !strings.Contains(sCalls[0].Arguments, `"owner":"ninhlee99"`) || !strings.Contains(sCalls[0].Arguments, `"repo":"amux"`) {
		t.Fatalf("expected owner=ninhlee99 and repo=amux, got: %s", sCalls[0].Arguments)
	}
}


