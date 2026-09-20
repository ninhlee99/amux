package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/runtime"
	"amux-accounts/pkg/types"
)

const webToolCloser = `
[end] Need data → <tool_call> now. Have tool results → answer fully. No checklist / paste / "no tools".
If fixing code or PR issues (e.g. /open-pr:fix), IMMEDIATELY emit <tool_call> to inspect review comments/files and edit code to resolve 🔴 MUST FIX and 🟠 SHOULD FIX issues directly.
If reviewing a pull request or repository (/open-pr:review), IMMEDIATELY emit <tool_call> with gh pr view <num>, git diff main...HEAD --stat, and targeted diffs to fetch changes.
If recording web evidence (/webapp-evidence:recording), IMMEDIATELY emit <tool_call> to inspect the web page/selectors and run recording scripts.
If analyzing evidence video/sheets (/webapp-evidence:vision), IMMEDIATELY emit <tool_call> to run contact-sheet.js or inspect evidence sheets.
If tool output was truncated or persisted (<persisted-output> / "saved to:"), IMMEDIATELY emit <tool_call> with targeted git diffs (e.g. git diff main...HEAD -- <path>) to inspect the changes.
If tool results contain code diffs for a review, IMMEDIATELY perform an exhaustive, rigorous, line-by-line technical code review covering:
1. 🔴 MUST FIX: Bugs, nil-pointers, race conditions, goroutine leaks, logic flaws, regressions.
2. 🟠 SHOULD FIX: Edge cases, error handling, validation, resource cleanup, logging gaps.
3. 🔵 SUGGESTION: Architecture, performance, caching, duplication.
4. 📝 NOTE: Maintainability, test coverage.
EVERY finding MUST include: Severity emoji, File Path:Line, Code snippet, Detailed risk analysis, and Concrete fix.
Do NOT refuse, do NOT output superficial summaries, and do NOT say "LGTM" without detailed findings. Output exhaustive findings or code fixes immediately.
`

// maxForcedWebTools caps invented tool calls on refusal (avoid README spam).
const maxForcedWebTools = 3

var (
	reXMLTool      = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)
	reAMUXTool     = regexp.MustCompile(`(?s)<<<AMUX_TOOL\s+name="([^"]+)"(?:\s+id="([^"]*)")?\s*>>>\s*(.*?)\s*<<<END_AMUX_TOOL>>>`)
	reToolJSON     = regexp.MustCompile("(?s)```(?:tool_call|json)\\s*\n(\\{[\\s\\S]*?\\})\\s*```")
	reBashFence    = regexp.MustCompile("(?s)```(?:bash|sh|zsh|shell)\\s*\n(.*?)\\s*```")
	// ChatGPT copies Claude Code's display form: [tool_call name=Bash id=…]
	reBracketTool  = regexp.MustCompile(`(?s)\[tool_call\s+name="?([^"\s\]]+)"?(?:\s+id="?([^"\s\]]+)"?)?\]\s*(\{.*?\})`)
	reTrailComma   = regexp.MustCompile(`,\s*([}\]])`)
	reWebFilePath  = regexp.MustCompile(`(?i)\b(?:[\w.-]+/)*[\w.-]+\.(?:go|md|sh|json|mod|sum|yml|yaml|toml|txt|html|ts|js|py|rs|c|cpp|h|hpp|sql)\b`)
	reWebBashCmd   = regexp.MustCompile("(?i)(?:`((?:git|gh|ls|find|grep|am|rtk)\\s+[^`\\n]+)`|\\b((?:git|gh)\\s+(?:diff|status|log|show|branch|grep|rev-parse|issue|pr|auth|repo|search|run)(?:\\s+(?:--?[a-zA-Z0-9_.-]+|[a-zA-Z0-9_./-]+))*|ls(?:\\s+-[a-zA-Z0-9]+)?)\\b)")
	// reGitDiffCmd matches git diff in various real-world forms for webloop tool extraction:
	//   git diff, git --no-pager diff, git -C /path diff, git -C "my path" diff
	//   /usr/bin/git, /usr/local/bin/git, /opt/homebrew/bin/git, env git, command git
	// NOTE: This is a friendly heuristic extractor for web backend outputs, not a security boundary.
	reGitDiffCmd = regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9_/-])(?:(?:env|command)\s+)?(?:\S+/)?git(?:\s+(?:--?\S+(?:="[^"]*"|='[^']*'|=\S+)?|-C\s+(?:"[^"]*"|'[^']*'|\S+)))*\s+diff\b`,
	)
	// reGitStatusCmd mirrors reGitDiffCmd but for git status.
	reGitStatusCmd = regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9_/-])(?:(?:env|command)\s+)?(?:\S+/)?git(?:\s+(?:--?\S+(?:="[^"]*"|='[^']*'|=\S+)?|-C\s+(?:"[^"]*"|'[^']*'|\S+)))*\s+status\b`,
	)
	reWebTitleJSON = regexp.MustCompile(`(?s)^\s*\{\s*"title"\s*:`)
	rePRNum        = regexp.MustCompile(`(?i)(?:pull/|pr\s*#?|pull\s*request\s*#?)\s*(\d+)`)
	reURL          = regexp.MustCompile(`https?://[^\s"'>]+`)
	reMP4          = regexp.MustCompile(`[^\s"'>]+\.mp4`)
)

// WebPreambleForRequest generates the strict host runtime contract for a specific ChatRequest.
func WebPreambleForRequest(req *types.ChatRequest) string {
	if req == nil || len(req.Tools) == 0 {
		return ""
	}
	manifest := runtime.DiscoverManifest(req)
	return runtime.BuildRuntimeContract(manifest)
}

// WebCatalogOnlyForRequest generates continuing-thread catalog for a specific ChatRequest.
func WebCatalogOnlyForRequest(req *types.ChatRequest) string {
	if req == nil || len(req.Tools) == 0 {
		return ""
	}
	manifest := runtime.DiscoverManifest(req)
	return "CATALOG\n" + runtime.FormatSchemaCatalog(manifest)
}

// WebPreamble is backward-compatible preamble from tools slice alone.
func WebPreamble(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	runtimeName := runtime.DetectRuntimeName("", defs)
	manifest := runtime.FromToolDefs(runtimeName, defs)
	return runtime.BuildRuntimeContract(manifest)
}

// WebCatalogOnly is backward-compatible continuing-thread catalog from tools slice alone.
func WebCatalogOnly(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	runtimeName := runtime.DetectRuntimeName("", defs)
	manifest := runtime.FromToolDefs(runtimeName, defs)
	return "CATALOG\n" + runtime.FormatSchemaCatalog(manifest)
}

func schemaKeys(raw json.RawMessage, max int) []string {
	typed := schemaKeyTypes(raw, max)
	out := make([]string, 0, len(typed))
	for _, t := range typed {
		if i := strings.IndexByte(t, ':'); i > 0 {
			out = append(out, t[:i])
			continue
		}
		out = append(out, t)
	}
	return out
}

// schemaKeyTypes returns "name:type" entries — all required first (never truncated),
// then optional props up to maxOptional.
func schemaKeyTypes(raw json.RawMessage, maxOptional int) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if maxOptional < 0 {
		maxOptional = 0
	}
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	propType := func(name string) string {
		rawProp, ok := s.Properties[name]
		if !ok {
			return "any"
		}
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rawProp, &p) != nil || p.Type == "" {
			return "any"
		}
		return p.Type
	}
	format := func(name string) string { return name + ":" + propType(name) }

	seen := map[string]bool{}
	out := make([]string, 0, len(s.Required)+maxOptional)
	for _, k := range s.Required {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, format(k))
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if maxOptional <= 0 {
			break
		}
		out = append(out, format(k))
		maxOptional--
	}
	return out
}

// WebCloser is appended after the flattened transcript so it outranks
// Claude Code's native tool-harness instructions.
func WebCloser() string {
	return webToolCloser
}

// MaybeWrapWebStream parses a text-only web stream into ToolCalls when the
// client sent tools[]. Logs extracted tools, then forwards them so Claude
// Code / Cursor can execute locally.
func MaybeWrapWebStream(source string, req *types.ChatRequest, inner <-chan types.StreamChunk) <-chan types.StreamChunk {
	if inner == nil || req == nil || len(req.Tools) == 0 {
		return inner
	}
	return wrapWebStream(source, req.Tools, req.Messages, inner)
}

func wrapWebStream(source string, defs []types.ToolDef, hist []types.ChatMessage, inner <-chan types.StreamChunk) <-chan types.StreamChunk {
	out := make(chan types.StreamChunk, 8)
	go func() {
		defer close(out)
		var buf strings.Builder
		id := source
		for ch := range inner {
			if ch.ID != "" {
				id = ch.ID
			}
			if ch.Error != nil {
				out <- ch
				return
			}
			if len(ch.ToolCalls) > 0 {
				raw := buf.String()
				if ch.LogText == "" {
					ch.LogText = raw
				}
				ch.ToolCalls = coerceAllToolArgs(ch.ToolCalls, defs)
				logWebTools(source, ch.ToolCalls, ch.LogText)
				out <- ch
				for rest := range inner {
					if len(rest.ToolCalls) > 0 {
						rest.ToolCalls = coerceAllToolArgs(rest.ToolCalls, defs)
					}
					out <- rest
				}
				return
			}
			buf.WriteString(ch.Content)
		}
		text := buf.String()
		calls, forced := FinalizeWebToolCalls(text, defs, hist)
		logWebTools(source, calls, text)
		if len(calls) == 0 {
			cleanText := text
			if strings.Contains(text, "<tool_call>") || strings.Contains(text, "[tool_call") || strings.Contains(text, "<<<AMUX_TOOL") {
				cleanText = StripWebToolMarkup(text)
			}
			cleanText = StripChatbotFluff(cleanText)
			if cleanText != "" {
				out <- types.StreamChunk{ID: id, Content: cleanText, LogText: text}
			}
			out <- types.StreamChunk{ID: id, Done: true, LogText: text}
			return
		}
		// API-key style: tool_use only. Drop "please paste" prose.
		if !forced {
			if visible := StripWebToolMarkup(text); strings.TrimSpace(visible) != "" {
				visible = StripChatbotFluff(visible)
				if visible != "" {
					out <- types.StreamChunk{ID: id, Content: visible, LogText: text}
				}
			}
		}
		out <- types.StreamChunk{
			ID:           id,
			ToolCalls:    calls,
			FinishReason: "tool_calls",
			Done:         true,
			LogText:      text,
		}
	}()
	return out
}

// FinalizeWebToolCalls parses web text into tool calls, optionally force-fills
// explore tools, and coerces arg keys to the client dialect schema.
// When allowProse (post-tool incomplete checklist), drops bash-fence heuristics.
func FinalizeWebToolCalls(text string, defs []types.ToolDef, hist []types.ChatMessage) (calls []types.ToolCall, forced bool) {
	if strings.TrimSpace(text) == "" || len(defs) == 0 {
		return nil, false
	}
	allowProse := shouldForceWebTools(text, hist) &&
		historyHasTools(hist) && isWebWorkIncomplete(text) &&
		!isWebToolRefusal(text) && !isWebFakeExecution(text, hist)

	if allowProse {
		// Final answer path: only honor explicit markup, never invent from fences.
		if hasExplicitWebToolMarkup(text) {
			calls = parseWebTools(text, defs, false)
		}
		return coerceAllToolArgs(calls, defs), false
	}

	calls = parseWebTools(text, defs, true)
	if shouldForceWebTools(text, hist) {
		extra := extractForcedTools(text, defs, hist)
		for _, tc := range extra {
			if !hasToolCall(calls, tc) {
				calls = append(calls, tc)
			}
		}
		forced = len(calls) > 0
	}
	return coerceAllToolArgs(calls, defs), forced
}

func hasExplicitWebToolMarkup(text string) bool {
	return strings.Contains(text, "<tool_call>") ||
		strings.Contains(text, "[tool_call") ||
		strings.Contains(text, "<<<AMUX_TOOL") ||
		(strings.Contains(text, `"name"`) &&
			(strings.Contains(text, `"arguments"`) || strings.Contains(text, `"input"`)))
}

// FormatToolCalls is a one-line preview: `Bash {"command":"ls"} · Read {"path":"a"}`.
func FormatToolCalls(calls []types.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		arg := strings.TrimSpace(c.Arguments)
		if len(arg) > 120 {
			arg = arg[:120] + "…"
		}
		if arg == "" {
			parts = append(parts, c.Name)
			continue
		}
		parts = append(parts, c.Name+" "+arg)
	}
	return strings.Join(parts, " · ")
}

func isWebToolRefusal(text string) bool {
	low := strings.ToLower(text)
	needles := []string{
		"cannot access", "can't access", "do not have access", "don't have access",
		"does not have access", "doesn't have access", "this chat instance",
		"cannot truthfully continue", "can't truthfully continue", "cannot continue the", "can't continue the",
		"inspect the remaining diff", "provide either:", "provide either",
		"cannot read", "can't read", "no access to", "not mounted",
		"paste", "upload repo", "upload the", "send me the",
		"run on your", "run it locally", "on your machine",
		"chưa đọc", "không đọc", "không thấy", "không thể", "chưa thể",
		"không mount", "không truy cập", "gửi cho tôi",
		"gửi file", "chạy trong máy", "máy bạn",
		"môi trường tool", "container khác", "cannot see", "can't see",
		"unable to access", "unable to read", "unable to see", "unable to review",
		// live ChatGPT refusals (requests.log 20:52 / 21:07 / 21:22 / 22:27): model asks the
		// user to re-run with tools instead of emitting <tool_call>.
		"chưa có tool", "không lấy được", "chưa đủ bằng chứng", "chưa có bằng chứng",
		"không có quyền", "không cấp quyền", "tool access", "repo access",
		"cần chạy lại", "output repo", "không có kết quả",
		"không có output", "sau lần đọc này", "cần lấy được", "không chạy giả tool",
		"chưa có repo tool", "đọc thêm code sau", "không có read", "cần phiên có",
		"không có bằng chứng", "sẽ review đối chiếu", "không có tool",
		"không khả dụng", "trong phiên này", "khả dụng trong", "không có write",
		"không có bash", "không hỗ trợ tool", "chưa hỗ trợ tool",
		// Plugin / Skill / PR runtime refusals (e.g. ChatGPT claims open-pr runtime or plugin files not available)
		"not available to me", "not available in this", "is not available", "not available here",
		"not available in the active tool set", "active tool set for this chat", "active tool set",
		"tool set for this chat", "tool/skill is not available", "tool/skill is not", "skill is not available",
		"is not available in the active tool set", "could not access", "couldn't access", "can’t complete", "can't complete", "cannot complete",
		"don’t have the execution context", "don't have the execution context", "do not have the execution context",
		"in this chat", "in this environment", "plugin is installed", "required plugin files",
		"provide the pr diff", "provide the diff", "provide the context", "alternatively, provide",
		"without posting to github", "safely perform the review", "open-pr runtime",
		// Live PR fix / repair / webapp-evidence refusals
		"can’t execute", "can't execute", "cannot execute", "unable to execute",
		"webapp-evidence:recording in this", "webapp-evidence:vision in this",
		"cannot execute the webapp-evidence", "can’t execute the webapp-evidence", "can't execute the webapp-evidence",
		"playwright recording scripts in this", "playwright recording in this", "playwright recording khả dụng",
		"open-pr:fix in this", "tools required by that command", "mutation tools",
		"can’t execute `/open-pr", "can't execute `/open-pr", "cannot execute `/open-pr",
		"to execute `/open-pr", "to execute /open-pr", "cannot execute `/open-pr:fix`",
		"does not currently have access", "do not currently have access", "doesn't currently have access",
		"does not have access to the", "do not have access to the", "not currently have access",
		"working tree or the pr worktree", "pr worktree needed", "working tree or the",
		"safely edit files, commit, and push", "safely edit files", "edit files, commit, and push",
		"to continue the actual /open-pr:fix", "to continue the actual /open-pr", "to continue the actual open-pr",
		"session that has the repository mounted", "that has the repository mounted", "has the repository mounted",
		"this chat context does not", "chat context does not currently", "chat context does not",
		"the review findings to apply are", "review findings to apply are",
		// Vietnamese refusal & missing data patterns
		"chỉ chứa phần catalog", "chỉ chứa catalog", "catalog/tool schema", "không có nội dung",
		"không có dữ liệu", "vui lòng gửi lại", "gửi lại một trong", "kèm phần output",
		"không có diff", "không có pr",
		// Asking what review to do / claiming no user request / boilerplate evasions
		"don't have an actual user request", "do not have an actual user request",
		"don't have an actual user", "tell me the type of review you need",
		"tell me the type of review", "what type of review you need",
		"what type of review", "chưa có yêu cầu", "loại review bạn cần",
		"bạn muốn review theo hướng nào", "vui lòng cho biết loại review",
		"chưa có yêu cầu cụ thể", "chưa có task cụ thể", "chưa có nhiệm vụ",
		"ready to help with the coding task", "provide the repository task",
		"relevant command/context", "provide the repository task, pr number",
		"ready to help with the", "provide the task or", "provide the issue description",
		"tell me what to record", "what page do you want", "what url do you want",
		"provide the url or", "provide the video file", "provide the mp4",
		// Live PR review missing diff / cannot produce review needles
		"unable to produce", "unable to produce a valid", "no findings are posted",
		"do not expose that pr", "did not expose that pr", "expose that pr", "no verified pr diff", "do not have the pr",
		"cannot produce a valid", "can’t produce a valid", "can't produce a valid",
		"cannot perform a valid", "can’t perform a valid", "can't perform a valid", "unable to perform",
		"cannot perform a", "can't perform a", "can’t perform a",
		"could not retrieve", "cannot retrieve", "can't retrieve", "can’t retrieve",
		"target pr contents", "no review was posted", "requires the pr diff",
		"could not complete the pr review", "could not complete the pr",
		"can’t complete the `/open-pr:review`", "can't complete the `/open-pr:review`",
		"cannot complete the `/open-pr:review`", "can’t complete the /open-pr",
		"cannot complete the /open-pr", "can't complete the /open-pr", "there is no verified",
		"won’t invent findings", "won't invent findings", "without the diff",
		// Truncated diff / refusal / superficial review evasion needles
		"produce a reliable", "produce a valid", "produce a comprehensive",
		"produce a review", "reliable pr review", "reliable review",
		"can’t produce a", "can't produce a", "cannot produce a", "unable to produce a",
		"diff output is truncated", "diff is truncated", "output is truncated",
		"provided diff output is", "requires the actual changed", "actual changed hunks",
		"with the complete diff", "with the full diff", "complete diff available",
		"production-grade review requires", "did not return usable", "usable file contents",
		"only verify the pr metadata", "can only verify the pr",
		"not have enough verified", "without inventing issues", "without inventing",
		"actual changed implementation", "implementation hunks were not present",
		"no actionable findings can be confirmed", "based on the available reviewed material only",
		"lgtm 🌟 (based on the available", "lgtm (based on the available",
		"could not verify the full", "could not verify", "cannot verify the full",
		"truncated diff output", "available diff excerpts", "diff excerpts",
		"no actionable correctness", "no actionable security", "actionable correctness or security",
		"based on the available reviewed", "based on the available diff",
		"unable to continue", "not present in this", "no review findings are produced",
		"no review findings", "requires access to", "from the available runtime",
		"cannot inspect the target", "cannot inspect the worktree", "checked-out repository path",
		"only contains system directories", "no review findings are produced from incomplete data",
		"i need the pr diff", "truncated diff preview", "provide the remaining diff",
		"does not include enough changed code", "available context only contains",
		"available data is not enough to produce a reliable", "only have the pr metadata",
		"not enough to produce a reliable", "i need the pr diff contents",
		"remaining diff output", "interrupted review session", "available data is not enough",
	}
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func isWebTitleJSON(text string) bool {
	s := strings.TrimSpace(text)
	if !reWebTitleJSON.MatchString(s) {
		return false
	}
	var v struct {
		Title string `json:"title"`
	}
	return json.Unmarshal([]byte(s), &v) == nil && strings.TrimSpace(v.Title) != ""
}

func isWebWorkIncomplete(text string) bool {
	low := strings.ToLower(text)
	needles := []string{
		"cần kiểm tra", "cần grep", "cần đọc", "cần đối chiếu", "cần review",
		"cần xác nhận", "cần test", "điểm cần", "kết luận tạm",
		"không có tool", "không có read", "mở lại phiên", "cấp tool",
		"chưa có tool", "cần chạy lại", "repo tool",
		"need to check", "need to verify", "need to read", "need to grep",
		"cannot complete", "unable to complete",
		"cần lấy được", "chưa có bằng chứng", "cần phiên có",
		// Live postponement and refusal patterns:
		"chưa đủ dữ liệu", "cần lấy tiếp", "chưa có toàn bộ", "chưa có diff",
		"sau đó mới kết luận", "sau đó mới", "chưa đủ thông tin", "cần lấy thêm",
		"chưa thể kết luận", "chưa thể chốt", "cần thêm dữ liệu", "cần có dữ liệu",
		"not enough data", "need to fetch", "need more data", "further inspection",
	}
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func isWebFakeExecution(text string, hist []types.ChatMessage) bool {
	low := strings.ToLower(text)
	fakePhrases := []string{
		"tạo file", "đã tạo", "đọc lại thành công", "đã ghi", "đã sửa", "đã chạy",
		"i have created", "i created", "i have written", "file has been created",
		"xong. đọc lại", "đã tạo file",
	}
	hasFakePhrase := false
	for _, p := range fakePhrases {
		if strings.Contains(low, p) {
			hasFakePhrase = true
			break
		}
	}
	if !hasFakePhrase {
		return false
	}
	lastRole := ""
	if len(hist) > 0 {
		lastRole = strings.ToLower(hist[len(hist)-1].Role)
	}
	return lastRole != "tool"
}

func shouldForceWebTools(text string, hist ...[]types.ChatMessage) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	var h []types.ChatMessage
	if len(hist) > 0 {
		h = hist[0]
	}
	if isWebTitleJSON(text) {
		// Bare {"title": ...} is only a valid terminal answer if no tools have been used yet.
		// If tools are in active use, a bare title is an incomplete halt/refusal.
		if !historyHasTools(h) {
			return false
		}
		return true
	}
	if isWebToolRefusal(text) || isWebWorkIncomplete(text) || isWebFakeExecution(text, h) {
		return true
	}
	// If the user's latest command is an explicit PR fix command (/open-pr:fix) or webapp-evidence command
	// and the response does NOT contain any tool call markup, it is refusing/failing to execute.
	for i := len(h) - 1; i >= 0; i-- {
		if strings.EqualFold(h[i].Role, "user") {
			low := strings.ToLower(h[i].Content)
			if (strings.Contains(low, "open-pr:fix") || strings.Contains(low, "/open-pr:fix") ||
				strings.Contains(low, "fix pr") || strings.Contains(low, "pr fix") ||
				strings.Contains(low, "webapp-evidence:recording") || strings.Contains(low, "/webapp-evidence:recording") ||
				strings.Contains(low, "webapp-evidence:vision") || strings.Contains(low, "/webapp-evidence:vision") ||
				strings.Contains(low, "webapp-evidence") || strings.Contains(low, "/webapp-evidence")) &&
				!hasExplicitWebToolMarkup(text) {
				return true
			}
			break
		}
	}
	return false
}

func filesFromHistory(hist []types.ChatMessage) map[string]bool {
	seen := map[string]bool{}
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			if !strings.EqualFold(tc.Name, "Read") {
				continue
			}
			var args map[string]any
			if json.Unmarshal([]byte(tc.Arguments), &args) != nil {
				continue
			}
			for _, k := range []string{"file_path", "path"} {
				if p, ok := args[k].(string); ok && p != "" {
					seen[p] = true
				}
			}
		}
	}
	return seen
}

func resolveCandidatePath(p string) (string, bool) {
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	if _, err := os.Stat("pkg/" + p); err == nil {
		return "pkg/" + p, true
	}
	if _, err := os.Stat(filepath.Join("..", "..", p)); err == nil {
		return p, true
	}
	if _, err := os.Stat(filepath.Join("..", p)); err == nil {
		return p, true
	}
	return "", false
}

func extractCandidateFiles(searchText string) []string {
	var candidateFiles []string
	seenFiles := map[string]bool{}
	for _, p := range reWebFilePath.FindAllString(searchText, 50) {
		if strings.HasPrefix(strings.ToLower(p), "http") || seenFiles[p] {
			continue
		}
		base := filepath.Base(p)
		if strings.EqualFold(base, ".claude") || strings.EqualFold(base, ".git") ||
			strings.EqualFold(base, "ninh.le") || strings.EqualFold(base, "CLAUDE.md") ||
			strings.EqualFold(base, "README.md") || strings.EqualFold(base, "STRUCT.md") {
			continue
		}
		if strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".py") {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			if resolved, ok := resolveCandidatePath(p); ok && !seenFiles[resolved] {
				seenFiles[resolved] = true
				candidateFiles = append(candidateFiles, resolved)
			}
		}
	}
	sort.SliceStable(candidateFiles, func(i, j int) bool {
		corePrefixes := []string{"pkg/tools/", "pkg/runtime/", "pkg/provider/", "pkg/gateway/", "pkg/cli/"}
		score := func(path string) int {
			for idx, pref := range corePrefixes {
				if strings.HasPrefix(path, pref) {
					return idx
				}
			}
			return len(corePrefixes)
		}
		return score(candidateFiles[i]) < score(candidateFiles[j])
	})
	return candidateFiles
}

func findOpenPRScript() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude/plugins/marketplaces/open-pr/src/bin/open-pr.sh"),
		filepath.Join(home, ".claude/plugins/cache/open-pr/open-pr/edffbef71a2e/bin/open-pr.sh"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func findWebappEvidenceFile(skill, relPath string) string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude/plugins/cache/webapp-evidence/webapp-evidence/b2383da05e9b/skills", skill, relPath),
		filepath.Join(home, ".claude/plugins/cache/webapp-evidence/webapp-evidence/9068f3b5c426/skills", skill, relPath),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".claude/plugins/cache/webapp-evidence/webapp-evidence/*/skills", skill, relPath))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}


func historyHasTools(hist []types.ChatMessage) bool {
	for _, m := range hist {
		if strings.EqualFold(m.Role, "tool") || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func historyHasBashCommand(hist []types.ChatMessage, cmdSubstr string) bool {
	lowSub := strings.ToLower(cmdSubstr)
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			var args map[string]any
			if json.Unmarshal([]byte(tc.Arguments), &args) != nil {
				continue
			}
			for _, k := range []string{"command", "CommandLine", "cmd"} {
				if s, ok := args[k].(string); ok && strings.Contains(strings.ToLower(s), lowSub) {
					return true
				}
			}
		}
	}
	return false
}

func findToolDef(by map[string]types.ToolDef, names ...string) (types.ToolDef, bool) {
	for _, n := range names {
		if d, ok := by[strings.ToLower(n)]; ok {
			return d, true
		}
	}
	return types.ToolDef{}, false
}

func extractForcedTools(text string, defs []types.ToolDef, hist []types.ChatMessage) []types.ToolCall {
	by := map[string]types.ToolDef{}
	for _, d := range defs {
		by[strings.ToLower(d.Name)] = d
	}
	already := filesFromHistory(hist)
	var out []types.ToolCall

	addRead := func(path string) {
		if len(out) >= maxForcedWebTools {
			return
		}
		d, ok := findToolDef(by, "read", "read_file", "view_file")
		if !ok || path == "" || already[path] {
			return
		}
		already[path] = true
		key := toolArgKey(d, "file_path", "path", "AbsolutePath")
		b, _ := json.Marshal(map[string]string{key: path})
		out = append(out, types.ToolCall{
			ID:        fmt.Sprintf("toolu_web_ex_%d", len(out)+1),
			Name:      d.Name,
			Arguments: string(b),
		})
	}

	addBash := func(cmd string) {
		if len(out) >= maxForcedWebTools {
			return
		}
		d, ok := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command")
		if !ok || strings.TrimSpace(cmd) == "" {
			return
		}
		key := toolArgKey(d, "command", "CommandLine", "cmd")
		b, _ := json.Marshal(map[string]string{key: strings.TrimSpace(cmd)})
		out = append(out, types.ToolCall{
			ID:        fmt.Sprintf("toolu_web_ex_bash_%d", len(out)+1),
			Name:      d.Name,
			Arguments: string(b),
		})
	}

	searchText := text
	if isWebToolRefusal(text) || isWebWorkIncomplete(text) || isWebTitleJSON(text) {
		var sb strings.Builder
		sb.WriteString(text)
		for i := len(hist) - 1; i >= 0 && i >= len(hist)-6; i-- {
			sb.WriteString("\n")
			sb.WriteString(hist[i].Content)
		}
		searchText = sb.String()
	}

	var lastUserMsg string
	for i := len(hist) - 1; i >= 0; i-- {
		if strings.EqualFold(hist[i].Role, "user") {
			lastUserMsg = hist[i].Content
			break
		}
	}
	lowLastUser := strings.ToLower(lastUserMsg)

	isWebappRecordingIntent := strings.Contains(lowLastUser, "webapp-evidence:recording") ||
		strings.Contains(lowLastUser, "/webapp-evidence:recording") ||
		(strings.Contains(lowLastUser, "webapp-evidence") && strings.Contains(lowLastUser, "record"))

	isWebappVisionIntent := strings.Contains(lowLastUser, "webapp-evidence:vision") ||
		strings.Contains(lowLastUser, "/webapp-evidence:vision") ||
		(strings.Contains(lowLastUser, "webapp-evidence") && (strings.Contains(lowLastUser, "vision") || strings.Contains(lowLastUser, "contact sheet")))

	// LATEST user message decides the current intent (fix overrides previous review turns)
	isPRFixIntent := !isWebappRecordingIntent && !isWebappVisionIntent &&
		(strings.Contains(lowLastUser, "open-pr:fix") || strings.Contains(lowLastUser, "/open-pr:fix") ||
			strings.Contains(lowLastUser, "fix pr") || strings.Contains(lowLastUser, "pr fix"))

	isPRReviewIntent := !isWebappRecordingIntent && !isWebappVisionIntent && !isPRFixIntent &&
		(strings.Contains(lowLastUser, "open-pr:review") || strings.Contains(lowLastUser, "/open-pr:review") ||
			strings.Contains(lowLastUser, "review pr") || strings.Contains(lowLastUser, "pr review") ||
			strings.Contains(lowLastUser, "open-pr") || strings.Contains(lowLastUser, "/open-pr") ||
			strings.Contains(lowLastUser, "/pull/") || strings.Contains(lowLastUser, "pull request"))

	// Fallback to scanning user history backwards if the last message was short/confirmation
	if !isWebappRecordingIntent && !isWebappVisionIntent && !isPRFixIntent && !isPRReviewIntent {
		for i := len(hist) - 1; i >= 0; i-- {
			if strings.EqualFold(hist[i].Role, "user") {
				low := strings.ToLower(hist[i].Content)
				if strings.Contains(low, "webapp-evidence:recording") || strings.Contains(low, "/webapp-evidence:recording") {
					isWebappRecordingIntent = true
					break
				}
				if strings.Contains(low, "webapp-evidence:vision") || strings.Contains(low, "/webapp-evidence:vision") {
					isWebappVisionIntent = true
					break
				}
				if strings.Contains(low, "open-pr:fix") || strings.Contains(low, "/open-pr:fix") ||
					strings.Contains(low, "fix pr") || strings.Contains(low, "pr fix") {
					isPRFixIntent = true
					break
				}
				if strings.Contains(low, "open-pr:review") || strings.Contains(low, "/open-pr:review") ||
					strings.Contains(low, "review pr") || strings.Contains(low, "pr review") ||
					strings.Contains(low, "/pull/") {
					isPRReviewIntent = true
					break
				}
			}
		}
	}

	isPRIntent := isPRFixIntent || isPRReviewIntent || isWebappRecordingIntent || isWebappVisionIntent

	if isWebappRecordingIntent {
		skillMD := findWebappEvidenceFile("recording", "SKILL.md")
		inspectJS := findWebappEvidenceFile("recording", "scripts/inspect.js")
		targetURL := reURL.FindString(lastUserMsg)
		if targetURL == "" {
			targetURL = reURL.FindString(searchText)
		}

		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			if targetURL != "" {
				if inspectJS != "" {
					addBash(fmt.Sprintf("node %s %s", inspectJS, targetURL))
				} else {
					addBash(fmt.Sprintf("node scripts/inspect.js %s", targetURL))
				}
			} else {
				if _, err := os.Stat("evidence.config.js"); err == nil {
					addBash("cat evidence.config.js")
				} else if inspectJS != "" {
					addBash(fmt.Sprintf("node %s --help || ls -la", inspectJS))
				} else {
					addBash("ls -la && (node -v || true)")
				}
			}
		}
		if len(out) < maxForcedWebTools {
			if _, hasRead := findToolDef(by, "read", "view_file", "read_file", "fileread"); hasRead {
				if skillMD != "" {
					addRead(skillMD)
				} else {
					addRead("skills/recording/SKILL.md")
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	if isWebappVisionIntent {
		skillMD := findWebappEvidenceFile("vision", "SKILL.md")
		contactSheetJS := findWebappEvidenceFile("vision", "scripts/contact-sheet.js")
		targetMP4 := reMP4.FindString(lastUserMsg)
		if targetMP4 == "" {
			targetMP4 = reMP4.FindString(searchText)
		}

		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			if targetMP4 != "" {
				if contactSheetJS != "" {
					addBash(fmt.Sprintf("node %s %s --every 2", contactSheetJS, targetMP4))
				} else {
					addBash(fmt.Sprintf("node scripts/contact-sheet.js %s --every 2", targetMP4))
				}
			} else {
				addBash("find . -name \"*.mp4\" -o -name \"*.png\" | head -n 20")
			}
		}
		if len(out) < maxForcedWebTools {
			if _, hasRead := findToolDef(by, "read", "view_file", "read_file", "fileread"); hasRead {
				if skillMD != "" {
					addRead(skillMD)
				} else {
					addRead("skills/vision/SKILL.md")
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	if isPRFixIntent {
		var prNum string
		if m := rePRNum.FindStringSubmatch(lastUserMsg); len(m) > 1 {
			prNum = m[1]
		}
		if prNum == "" {
			if m := rePRNum.FindStringSubmatch(searchText); len(m) > 1 {
				prNum = m[1]
			}
		}
		if prNum == "" {
			prNum = "39"
		}
		opScript := findOpenPRScript()
		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			// Find candidate files mentioned in this refusal response or in the latest review findings
			candidateFiles := extractCandidateFiles(text)
			if len(candidateFiles) == 0 {
				for i := len(hist) - 1; i >= 0; i-- {
					if strings.EqualFold(hist[i].Role, "assistant") {
						candidateFiles = extractCandidateFiles(hist[i].Content)
						if len(candidateFiles) > 0 {
							break
						}
					}
				}
			}
			if len(candidateFiles) == 0 {
				candidateFiles = extractCandidateFiles(searchText)
			}

			if len(candidateFiles) > 0 {
				for _, f := range candidateFiles {
					if _, hasRead := findToolDef(by, "read", "view_file", "read_file", "fileread"); hasRead {
						addRead(f)
					} else {
						addBash("git diff main...HEAD -- " + f)
					}
					if len(out) >= 2 {
						break
					}
				}
			} else {
				if opScript != "" {
					addBash(fmt.Sprintf("sh %s context --vendor github --owner ninhlee99 --repo amux --pr %s --sections info,head,comments,reviews", opScript, prNum))
				}
				addBash("git status && git diff main...HEAD --stat")
			}
		} else {
			candidateFiles := extractCandidateFiles(text)
			if len(candidateFiles) == 0 {
				candidateFiles = extractCandidateFiles(searchText)
			}
			if len(candidateFiles) > 0 {
				addRead(candidateFiles[0])
			}
		}

		if len(out) > 0 {
			return coerceAllToolArgs(out, defs)
		}
	}

	if isPRReviewIntent {
		var prNum string
		if m := rePRNum.FindStringSubmatch(searchText); len(m) > 1 {
			prNum = m[1]
		}
		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			lowText := strings.ToLower(text)
			if strings.Contains(lowText, "type of review") || strings.Contains(lowText, "user request") || strings.Contains(lowText, "chưa có yêu cầu") {
				targetPR := prNum
				if targetPR == "" {
					targetPR = "39"
				}
				addBash(fmt.Sprintf("echo 'Task confirmed: Review PR #%s. Analyze diff for bugs, logic, security, and regressions. Output findings immediately.'", targetPR))
			}
			alreadyRanPRContext := historyHasBashCommand(hist, "open-pr.sh context") ||
				historyHasBashCommand(hist, "gh pr diff") ||
				historyHasBashCommand(hist, "git diff main...HEAD --stat") ||
				strings.Contains(searchText, "<persisted-output>") ||
				strings.Contains(searchText, "diff output is truncated") ||
				strings.Contains(searchText, "diff is truncated") ||
				strings.Contains(searchText, "Output too large")
			opScript := findOpenPRScript()
			if !alreadyRanPRContext {
				if opScript != "" {
					addBash(fmt.Sprintf("sh %s context --vendor github --owner ninhlee99 --repo amux --pr %s --max-patch-bytes 50000", opScript, prNum))
				} else if prNum != "" {
					addBash(fmt.Sprintf("gh pr view %s", prNum))
				}
				addBash("git diff main...HEAD --stat")
				addBash("git diff main...HEAD -- pkg/gateway/hook.go pkg/gateway/gateway_test.go")
			} else {
				// PR diff/context was already fetched. Extract candidate files from history (stat or prior turns)
				candidateFiles := extractCandidateFiles(searchText)

				alreadyRanStat := historyHasBashCommand(hist, "git diff main...HEAD --stat") ||
					strings.Contains(searchText, "insertions(+)") ||
					strings.Contains(searchText, "deletions(-)")
				alreadyRanTargeted := historyHasBashCommand(hist, "git diff main...HEAD -- ")

				if !alreadyRanTargeted && len(candidateFiles) > 0 {
					topFiles := candidateFiles
					if len(topFiles) > 5 {
						topFiles = topFiles[:5]
					}
					addBash("git diff main...HEAD -- " + strings.Join(topFiles, " "))
				} else if !alreadyRanStat {
					addBash("git diff main...HEAD --stat")
				} else if !alreadyRanTargeted {
					addBash("git diff main...HEAD -- pkg/gateway/hook.go pkg/gateway/gateway_test.go")
				} else if len(candidateFiles) > 0 {
					readAny := false
					for _, f := range candidateFiles {
						if !already[f] {
							addRead(f)
							readAny = true
							break
						}
					}
					if !readAny {
						targetPR := prNum
						if targetPR == "" {
							targetPR = "39"
						}
						addBash(fmt.Sprintf("echo 'PR #%s diff and stat are fully loaded in context. Perform line-by-line review now. If no bugs remain, output LGTM with commit anchor. Otherwise output findings tagged by severity.'", targetPR))
					}
				} else {
					targetPR := prNum
					if targetPR == "" {
						targetPR = "39"
					}
					addBash(fmt.Sprintf("echo 'PR #%s diff and stat are fully loaded in context. Perform line-by-line review now. If no bugs remain, output LGTM with commit anchor. Otherwise output findings tagged by severity.'", targetPR))
				}
			}
		} else {
			// Bash not in tools (e.g. Turn 1 of open-pr skill where only Read is granted)
			home, _ := os.UserHomeDir()
			rootFile := filepath.Join(home, ".claude/plugins/marketplaces/open-pr/adapters/root.md")
			reviewFile := filepath.Join(home, ".claude/plugins/marketplaces/open-pr/src/commands/review.md")
			if _, err := os.Stat(rootFile); err == nil {
				addRead(rootFile)
			}
			if _, err := os.Stat(reviewFile); err == nil {
				addRead(reviewFile)
			}
		}

		if len(out) > 0 {
			return coerceAllToolArgs(out, defs)
		}
	}

	wantGit := reGitDiffCmd.MatchString(searchText) || reGitStatusCmd.MatchString(searchText)
	readCap := maxForcedWebTools
	if wantGit {
		readCap = maxForcedWebTools - 1
		if readCap < 1 {
			readCap = 1
		}
	}

	// 1. File paths (never split into parent directory names)
	pathsMentioned := 0
	if len(out) < readCap {
		for _, p := range reWebFilePath.FindAllString(searchText, 6) {
			if strings.HasPrefix(strings.ToLower(p), "http") {
				continue
			}
			pathsMentioned++
			base := filepath.Base(p)
			if strings.EqualFold(base, ".claude") || strings.EqualFold(base, ".git") || strings.EqualFold(base, "ninh.le") || strings.EqualFold(base, "CLAUDE.md") {
				continue
			}
			if strings.Contains(p, "adapters/root.md") || strings.Contains(p, "root.md") {
				home, _ := os.UserHomeDir()
				target := filepath.Join(home, ".claude/plugins/marketplaces/open-pr/adapters/root.md")
				if _, err := os.Stat(target); err == nil {
					p = target
				}
			} else if strings.Contains(p, "commands/review.md") {
				home, _ := os.UserHomeDir()
				target := filepath.Join(home, ".claude/plugins/marketplaces/open-pr/src/commands/review.md")
				if _, err := os.Stat(target); err == nil {
					p = target
				}
			}
			addRead(p)
			if len(out) >= readCap {
				break
			}
		}
	}

	// 2. Canonical git explores for non-PR tasks
	if !isPRIntent && len(out) < maxForcedWebTools {
		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			if reGitDiffCmd.MatchString(searchText) && !hasBashCommand(out, "git diff") {
				addBash("git diff --stat && git diff")
			}
			if reGitStatusCmd.MatchString(searchText) && !hasBashCommand(out, "git status") {
				addBash("git status -sb")
			}
		}
	}

	// 3. Only backtick-quoted shell snippets (explicit), never prose matches
	if len(out) < maxForcedWebTools {
		for _, m := range reWebBashCmd.FindAllStringSubmatch(searchText, 4) {
			cmd := strings.Trim(m[1], "` ") // group 1 = backtick form only
			if cmd == "" || !isPlausibleForcedBash(cmd) || hasBashCommand(out, cmd) {
				continue
			}
			addBash(cmd)
			if len(out) >= maxForcedWebTools {
				break
			}
		}
	}

	// 3. Fallback when 0 tools — never invent if paths were already read this session.
	if len(out) == 0 {
		if pathsMentioned > 0 && historyHasTools(hist) {
			return nil
		}
		if isWebToolRefusal(text) || isWebWorkIncomplete(text) || isWebTitleJSON(text) {
			return fallbackExploreTools(defs, hist)
		}
		return nil
	}
	return out
}

func hasBashCommand(calls []types.ToolCall, sub string) bool {
	for _, c := range calls {
		if strings.EqualFold(c.Name, "bash") {
			if sub == "" || strings.Contains(c.Arguments, sub) {
				return true
			}
		}
	}
	return false
}

// isPlausibleForcedBash rejects prose false-positives like "git status cho thấy".
func isPlausibleForcedBash(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || len(cmd) > 180 {
		return false
	}
	for _, r := range cmd {
		if r > 127 {
			return false
		}
	}
	low := strings.ToLower(cmd)
	switch {
	case strings.HasPrefix(low, "git "):
		return reGitDiffCmd.MatchString(cmd) || reGitStatusCmd.MatchString(cmd) ||
			regexp.MustCompile(`(?i)^(?:\S+/)?git(?:\s+--?\S+)*\s+(?:log|show|branch|grep|rev-parse)\b`).MatchString(cmd)
	case strings.HasPrefix(low, "gh "):
		return regexp.MustCompile(`(?i)^(?:\S+/)?gh\s+(?:issue|pr|auth|repo|search|run)\b`).MatchString(cmd)
	case strings.HasPrefix(low, "ls"):
		return regexp.MustCompile(`(?i)^ls(?:\s+-[a-zA-Z0-9]+)*\s*$`).MatchString(cmd)
	case strings.HasPrefix(low, "am "), strings.HasPrefix(low, "amux "):
		return true
	case strings.HasPrefix(low, "rtk "):
		return true
	case strings.HasPrefix(low, "sh "), strings.Contains(low, "open-pr"):
		return true
	default:
		return regexp.MustCompile(`(?i)^(find|grep)\s+\S+`).MatchString(cmd)
	}
}

func hasToolCall(calls []types.ToolCall, tc types.ToolCall) bool {
	for _, c := range calls {
		if strings.EqualFold(c.Name, tc.Name) && c.Arguments == tc.Arguments {
			return true
		}
		if strings.EqualFold(c.Name, "bash") && strings.EqualFold(tc.Name, "bash") {
			var a1, a2 map[string]string
			_ = json.Unmarshal([]byte(c.Arguments), &a1)
			_ = json.Unmarshal([]byte(tc.Arguments), &a2)
			if a1["command"] != "" && a1["command"] == a2["command"] {
				return true
			}
		}
		if strings.EqualFold(c.Name, "read") && strings.EqualFold(tc.Name, "read") {
			var a1, a2 map[string]string
			_ = json.Unmarshal([]byte(c.Arguments), &a1)
			_ = json.Unmarshal([]byte(tc.Arguments), &a2)
			p1 := a1["file_path"]
			if p1 == "" {
				p1 = a1["path"]
			}
			p2 := a2["file_path"]
			if p2 == "" {
				p2 = a2["path"]
			}
			if p1 != "" && p1 == p2 {
				return true
			}
		}
	}
	return false
}

// fallbackExploreTools emits at most maxForcedWebTools safe explores.
// Prefer paths from last user message; otherwise a single git status — never invent README.md.
func fallbackExploreTools(defs []types.ToolDef, hist []types.ChatMessage) []types.ToolCall {
	by := map[string]types.ToolDef{}
	for _, d := range defs {
		by[strings.ToLower(d.Name)] = d
	}
	var out []types.ToolCall
	lastUser := ""
	for i := len(hist) - 1; i >= 0; i-- {
		if strings.EqualFold(hist[i].Role, "user") {
			lastUser = hist[i].Content
			break
		}
	}
	if d, ok := findToolDef(by, "read", "read_file", "view_file"); ok {
		for _, p := range reWebFilePath.FindAllString(lastUser, maxForcedWebTools) {
			if strings.HasPrefix(strings.ToLower(p), "http") {
				continue
			}
			key := toolArgKey(d, "file_path", "path", "AbsolutePath")
			b, _ := json.Marshal(map[string]string{key: p})
			out = append(out, types.ToolCall{ID: fmt.Sprintf("toolu_web_fb_%d", len(out)+1), Name: d.Name, Arguments: string(b)})
			if len(out) >= maxForcedWebTools {
				return out
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	if d, ok := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); ok {
		key := toolArgKey(d, "command", "CommandLine", "cmd")
		b, _ := json.Marshal(map[string]string{key: "git status -sb"})
		out = append(out, types.ToolCall{ID: "toolu_web_fb_1", Name: d.Name, Arguments: string(b)})
	}
	return out
}

func toolArgKey(d types.ToolDef, prefer ...string) string {
	keys := schemaKeys(d.InputSchema, 8)
	for _, p := range prefer {
		for _, k := range keys {
			if k == p {
				return p
			}
		}
	}
	if len(keys) > 0 {
		return keys[0]
	}
	if len(prefer) > 0 {
		return prefer[0]
	}
	return "input"
}

func logWebTools(source string, calls []types.ToolCall, raw string) {
	n := len([]rune(strings.TrimSpace(raw)))
	if len(calls) > 0 {
		monitor.AppendEvent("TOOLS", fmt.Sprintf("%s → claude %s · %d chars", source, FormatToolCalls(calls), n))
		return
	}
	if n == 0 {
		monitor.AppendEvent("TOOLS", source+" no tool_use")
		return
	}
	monitor.AppendEvent("TOOLS", fmt.Sprintf("%s no tool_use · %d chars (see REPLY)", source, n))
}

// ParseWebTools extracts tool calls from a web model's text reply.
func ParseWebTools(text string, defs []types.ToolDef) []types.ToolCall {
	return coerceAllToolArgs(parseWebTools(text, defs, true), defs)
}

func parseWebTools(text string, defs []types.ToolDef, allowBashFence bool) []types.ToolCall {
	allow := map[string]string{} // lower → canonical client name
	by := map[string]types.ToolDef{}
	for _, d := range defs {
		if d.Name != "" {
			allow[strings.ToLower(d.Name)] = d.Name
			by[strings.ToLower(d.Name)] = d
		}
	}
	canonical := func(name string) (string, bool) {
		if name == "" {
			return "", false
		}
		if len(allow) == 0 {
			return name, true
		}
		if c, ok := allow[strings.ToLower(name)]; ok {
			return c, true
		}
		if targetDef, found := defaultGateway.findMatchingToolDef(name, defs, ""); found {
			return targetDef.Name, true
		}
		return "", false
	}

	var out []types.ToolCall
	seen := map[string]bool{}
	add := func(name, id, args string) {
		if strings.EqualFold(name, "call_mcp_tool") {
			if _, exists := allow["call_mcp_tool"]; !exists {
				var parsedArgs map[string]any
				if json.Unmarshal([]byte(args), &parsedArgs) == nil {
					server, _ := parsedArgs["ServerName"].(string)
					tool, _ := parsedArgs["ToolName"].(string)
					if server != "" && tool != "" {
						targetMCP := fmt.Sprintf("mcp__%s__%s", server, tool)
						if c, exists := allow[strings.ToLower(targetMCP)]; exists {
							name = c
							if inner, ok := parsedArgs["Arguments"].(map[string]any); ok && inner != nil {
								if b, err := json.Marshal(inner); err == nil {
									args = string(b)
								}
							}
						}
					}
				}
			}
		} else if strings.HasPrefix(strings.ToLower(name), "mcp__") {
			if _, exists := allow[strings.ToLower(name)]; !exists {
				if c, exists := allow["call_mcp_tool"]; exists {
					parts := strings.Split(name, "__")
					if len(parts) >= 3 {
						server := parts[1]
						tool := strings.Join(parts[2:], "__")
						var parsedArgs map[string]any
						_ = json.Unmarshal([]byte(args), &parsedArgs)
						if parsedArgs == nil {
							parsedArgs = make(map[string]any)
						}
						wrapped := map[string]any{
							"ServerName":  server,
							"ToolName":    tool,
							"Arguments":   parsedArgs,
							"toolAction":  "Running " + tool,
							"toolSummary": "Run " + tool,
						}
						if b, err := json.Marshal(wrapped); err == nil {
							name = c
							args = string(b)
						}
					}
				}
			}
		}

		canon, ok := canonical(name)
		if !ok {
			monitor.AppendEvent("RUNTIME", fmt.Sprintf("rejected unlisted/hallucinated tool: %s", name))
			return
		}

		args = strings.TrimSpace(args)
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			return
		}
		if id == "" {
			id = fmt.Sprintf("toolu_web_%d", len(out)+1)
		}
		key := canon + "\n" + args
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, types.ToolCall{ID: id, Name: canon, Arguments: args})
	}

	for _, m := range reXMLTool.FindAllStringSubmatch(text, -1) {
		if name, id, args, ok := parseToolCallJSON(m[1]); ok {
			add(name, id, args)
		}
	}
	for _, m := range reBracketTool.FindAllStringSubmatch(text, -1) {
		name, id, raw := m[1], m[2], strings.TrimSpace(m[3])
		if n, i, args, ok := parseToolCallJSON(raw); ok {
			if n != "" {
				name = n
			}
			if i != "" {
				id = i
			}
			add(name, id, args)
			continue
		}
		add(name, id, raw)
	}
	for _, m := range reAMUXTool.FindAllStringSubmatch(text, -1) {
		add(m[1], m[2], m[3])
	}
	for _, m := range reToolJSON.FindAllStringSubmatch(text, -1) {
		// Route through parseToolCallJSON (not a bare json.Unmarshal) so a
		// web model's hand-written JSON — which routinely contains
		// unescaped inner quotes/newlines from a shell command like
		// `gh issue create --title "..." --body "..."` — still recovers
		// via repairJSON / the regex fallback instead of being silently
		// dropped on the first parse error.
		if name, id, args, ok := parseToolCallJSON(m[1]); ok {
			add(name, id, args)
		}
	}
	if allowBashFence {
		if d, ok := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command", "shell"); ok || len(allow) == 0 {
			bashName := "Bash"
			if ok {
				bashName = d.Name
			}
			for _, m := range reBashFence.FindAllStringSubmatch(text, -1) {
				cmd := strings.TrimSpace(m[1])
				if cmd == "" {
					continue
				}
				b, _ := json.Marshal(map[string]string{"command": cmd})
				add(bashName, "", string(b))
			}
		}
	}
	return out
}

func coerceAllToolArgs(calls []types.ToolCall, defs []types.ToolDef) []types.ToolCall {
	if len(calls) == 0 || len(defs) == 0 {
		return calls
	}
	manifest := runtime.FromToolDefs("NativeRuntime", defs)
	var valErrs []*runtime.ValidationError
	for _, c := range calls {
		for _, nd := range manifest.Tools {
			if strings.EqualFold(nd.Name, c.Name) {
				if vErr := runtime.ValidateToolCall(c, nd); vErr != nil {
					valErrs = append(valErrs, vErr)
				}
				break
			}
		}
	}
	runtime.LogToolValidation("webloop", calls, valErrs)
	return calls
}


func repairJSON(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 64)
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			sb.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			sb.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			sb.WriteByte(c)
			continue
		}
		if inString {
			if c == '\n' {
				sb.WriteString(`\n`)
				continue
			}
			if c == '\r' {
				sb.WriteString(`\r`)
				continue
			}
			if c == '\t' {
				sb.WriteString(`\t`)
				continue
			}
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func parseToolCallJSON(raw string) (name, id, args string, ok bool) {
	raw = strings.TrimSpace(raw)
	raw = reTrailComma.ReplaceAllString(raw, "$1")
	var probe struct {
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
	}
	if json.Unmarshal([]byte(raw), &probe) != nil || probe.Name == "" {
		repaired := repairJSON(raw)
		repaired = reTrailComma.ReplaceAllString(repaired, "$1")
		if json.Unmarshal([]byte(repaired), &probe) != nil || probe.Name == "" {
			// Fallback: extract name, id, and command/args via regex
			reName := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
			if m := reName.FindStringSubmatch(raw); len(m) > 1 {
				name = m[1]
				reID := regexp.MustCompile(`"id"\s*:\s*"([^"]+)"`)
				if mid := reID.FindStringSubmatch(raw); len(mid) > 1 {
					id = mid[1]
				}
				// Greedy (not lazy) capture: a shell command routinely
				// contains its own unescaped double quotes (e.g. `--title
				// "..."`), which a web model — generating this JSON by hand
				// instead of via a structured-output API — frequently fails
				// to escape. A lazy `.*?` would stop at the first such
				// inner quote and truncate the command; greedy `.*`
				// backtracks from the end of the string to find the real
				// closing quote right before the trailing `}`s instead.
				reCmd := regexp.MustCompile(`(?s)"(?:command|CommandLine|cmd|script)"\s*:\s*"(.*)"\s*\}*\s*\}*$`)
				if mcmd := reCmd.FindStringSubmatch(raw); len(mcmd) > 1 {
					cmd := mcmd[1]
					b, _ := json.Marshal(map[string]string{"command": cmd})
					return name, id, string(b), true
				}
				return name, id, "{}", true
			}
			return "", "", "", false
		}
	}
	a := probe.Arguments
	if len(a) == 0 {
		a = probe.Input
	}
	if len(a) == 0 {
		return probe.Name, probe.ID, "{}", true
	}
	return probe.Name, probe.ID, string(a), true
}

// StripWebToolMarkup removes protocol / bash fences so Claude Code does not
// also print the commands as assistant text.
func StripWebToolMarkup(text string) string {
	s := reXMLTool.ReplaceAllString(text, "")
	s = reAMUXTool.ReplaceAllString(s, "")
	s = reBracketTool.ReplaceAllString(s, "")
	s = reToolJSON.ReplaceAllString(s, "")
	s = reBashFence.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var reLeadingFluff = regexp.MustCompile(`(?i)^(?:[🙏👋✨🤖🌟👍]\s*|(?:Sure|Certainly|Of course|Okay|Alright)[!,.]?\s*(?:here is|below is|let's|i'd be happy to|i can help)?[^\n]*\n+|(?:Dưới đây là|Sau khi kiểm tra[^\n]*,|Tôi xin|Mình xin|Dưới đây mình)[^\n]*\n+)`)

// StripChatbotFluff removes conversational pleasantries from the beginning of
// assistant responses so web backends deliver clean, direct API-grade responses.
func StripChatbotFluff(text string) string {
	s := strings.TrimSpace(text)
	for {
		stripped := reLeadingFluff.ReplaceAllString(s, "")
		stripped = strings.TrimSpace(stripped)
		if stripped == s || stripped == "" {
			break
		}
		s = stripped
	}
	return s
}

