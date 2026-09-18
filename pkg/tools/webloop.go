package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/types"
)

// Protocol is tiny on purpose: catalog is rebuilt every request from the
// client's live tools[] (new MCP / plugin / Skill appear with no code change).
// Keep preamble short — every tools turn pays this cost on cold-start threads.
const webToolPreamble = `Coding-agent backend. Client executes tools locally on real repo. [Tool result] = verified output.
RULES:
1. Need command/file/search/PR context → emit <tool_call> immediately. Never claim inability, lack of runtime, or ask user to paste diffs.
2. For skills/plugins like /open-pr:review, /open-pr:fix: run shell commands ('gh pr diff', 'gh pr view', 'git diff') via <tool_call> to fetch PR context. You HAVE full terminal execution.
3. Must use EXACT tool name and property names from CATALOG below (do NOT rename arguments):
   - If tool is 'run_command': use {"CommandLine":"...", "Cwd":".", "WaitMsBeforeAsync":10000, "toolAction":"Running command", "toolSummary":"Execute shell"} (DO NOT use 'command').
   - If tool is 'Bash': use {"command":"..."}.
   - If tool is 'exec_command': use {"cmd":"..."}.
   - If tool is 'replace_file_content': use {"TargetFile":"...", "TargetContent":"...", "ReplacementContent":"..."}.
   - If tool is 'write_to_file': use {"TargetFile":"...", "CodeContent":"..."}.
   - If tool is 'view_file' or 'read_file': use {"AbsolutePath":"..."}.
3. Format:
<tool_call>
{"name":"TOOL","arguments":{...}}
</tool_call>
Multiple blocks OK. CATALOG
`

const webToolCloser = `
[end] Need data → <tool_call> now. Have tool results → answer fully. No checklist / paste / "no tools".
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
)

// WebPreamble is appended to a web-backend prompt when the client sent tools[].
func WebPreamble(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	return webToolPreamble + catalogBlock(defs)
}

// WebCatalogOnly is the continuing-thread preamble: live catalog, no rules essay.
func WebCatalogOnly(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	return "CATALOG\n" + catalogBlock(defs)
}

func catalogBlock(defs []types.ToolDef) string {
	sorted := make([]types.ToolDef, len(defs))
	copy(sorted, defs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	var b strings.Builder
	for _, d := range sorted {
		b.WriteString(catalogLine(d))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.String()
}

// catalogLine is "Name" or "Name:key:type,..." — live tools[], typed required args.
func catalogLine(d types.ToolDef) string {
	keys := schemaKeyTypes(d.InputSchema, 6)
	if len(keys) == 0 {
		return d.Name
	}
	return d.Name + ":" + strings.Join(keys, ",")
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
				ch.ToolCalls = NormalizeToolCalls(ch.ToolCalls, defs, "")
				logWebTools(source, ch.ToolCalls, ch.LogText)
				out <- ch
				for rest := range inner {
					if len(rest.ToolCalls) > 0 {
						rest.ToolCalls = NormalizeToolCalls(rest.ToolCalls, defs, "")
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
			if cleanText != "" {
				out <- types.StreamChunk{ID: id, Content: cleanText, LogText: text}
			}
			out <- types.StreamChunk{ID: id, Done: true, LogText: text}
			return
		}
		// API-key style: tool_use only. Drop "please paste" prose.
		if !forced {
			if visible := StripWebToolMarkup(text); strings.TrimSpace(visible) != "" {
				out <- types.StreamChunk{ID: id, Content: visible, LogText: text}
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
		"could not access", "couldn't access", "can’t complete", "can't complete", "cannot complete",
		"don’t have the execution context", "don't have the execution context", "do not have the execution context",
		"in this chat", "in this environment", "plugin is installed", "required plugin files",
		"provide the pr diff", "provide the diff", "provide the context", "alternatively, provide",
		"without posting to github", "safely perform the review", "open-pr runtime",
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
	return isWebToolRefusal(text) || isWebWorkIncomplete(text) || isWebFakeExecution(text, h)
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

func historyHasTools(hist []types.ChatMessage) bool {
	for _, m := range hist {
		if strings.EqualFold(m.Role, "tool") || len(m.ToolCalls) > 0 {
			return true
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
		for i := len(hist) - 1; i >= 0; i-- {
			if strings.EqualFold(hist[i].Role, "user") {
				searchText = text + "\n" + hist[i].Content
				break
			}
		}
	}

	pathsMentioned := 0
	wantGit := reGitDiffCmd.MatchString(searchText) || reGitStatusCmd.MatchString(searchText)
	// Reserve 1 slot for bash when git is mentioned so path spam cannot
	// crowd out the diff/status explore (review loops need both).
	readCap := maxForcedWebTools
	if wantGit {
		readCap = maxForcedWebTools - 1
		if readCap < 1 {
			readCap = 1
		}
	}

	// 1. File paths first (review/fix need files before more git spam)
	for _, p := range reWebFilePath.FindAllString(searchText, 6) {
		if strings.HasPrefix(strings.ToLower(p), "http") {
			continue
		}
		pathsMentioned++
		parts := strings.Split(p, "/")
		extParts := 0
		for _, part := range parts {
			if strings.Contains(part, ".") {
				extParts++
			}
		}
		if extParts > 1 {
			for _, part := range parts {
				if strings.Contains(part, ".") {
					addRead(part)
				}
			}
		} else {
			addRead(p)
		}
		if len(out) >= readCap {
			break
		}
	}

	// 2. Canonical git explores (before fuzzy bash — avoids "git status cho thấy")
	if len(out) < maxForcedWebTools {
		if _, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command"); hasBash {
			if reGitDiffCmd.MatchString(searchText) && !hasBashCommand(out, "git diff") {
				addBash("git diff --stat && git diff")
			}
			if reGitStatusCmd.MatchString(searchText) && !hasBashCommand(out, "git status") {
				addBash("git status -sb")
			}
			// Special handling for PR / open-pr refusal
			lowSearch := strings.ToLower(searchText)
			if strings.Contains(lowSearch, "open-pr") || strings.Contains(lowSearch, "pr review") ||
				strings.Contains(lowSearch, "pr diff") || strings.Contains(lowSearch, "/open-pr") {
				if !hasBashCommand(out, "gh pr diff") && !hasBashCommand(out, "git diff") {
					addBash("gh pr diff 2>/dev/null || git diff HEAD~1 2>/dev/null || git diff")
				}
				if !hasBashCommand(out, "gh pr view") && !hasBashCommand(out, "git status") {
					addBash("gh pr view 2>/dev/null || git status -sb")
				}
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
		lower := strings.ToLower(name)
		switch {
		case lower == "bash" || lower == "shell" || lower == "run_terminal_command" ||
			lower == "run_command" || lower == "exec_command":
			if d, ok := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command", "shell"); ok {
				return d.Name, true
			}
		case lower == "read" || lower == "read_file" || lower == "view_file":
			if d, ok := findToolDef(by, "read", "read_file", "view_file"); ok {
				return d.Name, true
			}
		case lower == "write" || lower == "write_file" || lower == "write_to_file":
			if d, ok := findToolDef(by, "write", "write_file", "write_to_file"); ok {
				return d.Name, true
			}
		case lower == "edit" || lower == "edit_file" || lower == "replace_file_content" ||
			lower == "patch" || lower == "str_replace_editor":
			if d, ok := findToolDef(by, "edit", "edit_file", "replace_file_content", "patch", "str_replace_editor"); ok {
				return d.Name, true
			}
		case lower == "grep" || lower == "grep_search" || lower == "search_code" || lower == "search":
			if d, ok := findToolDef(by, "grep", "grep_search", "search_code", "search"); ok {
				return d.Name, true
			}
		case lower == "find" || lower == "find_by_name" || lower == "glob" || lower == "file_search":
			if d, ok := findToolDef(by, "find", "find_by_name", "glob", "file_search"); ok {
				return d.Name, true
			}
		case lower == "agent" || lower == "invoke_subagent" || lower == "subagent" ||
			lower == "task" || lower == "spawn_agent" || lower == "dispatch_agent":
			if d, ok := findToolDef(by, "agent", "invoke_subagent", "subagent", "task", "spawn_agent", "dispatch_agent"); ok {
				return d.Name, true
			}
		case lower == "skill" || lower == "load_skill" || lower == "run_skill" || lower == "use_skill":
			if d, ok := findToolDef(by, "skill", "load_skill", "run_skill", "use_skill"); ok {
				return d.Name, true
			}
		}

		// MCP tool matching: e.g. "mcp__server__tool" <-> "server_tool" or "mcp_server_tool"
		cleanName := strings.TrimPrefix(lower, "mcp__")
		cleanName = strings.TrimPrefix(cleanName, "mcp_")
		cleanNameNorm := strings.ReplaceAll(strings.ReplaceAll(cleanName, "__", "_"), "-", "_")
		for k, canon := range allow {
			kClean := strings.TrimPrefix(k, "mcp__")
			kClean = strings.TrimPrefix(kClean, "mcp_")
			kCleanNorm := strings.ReplaceAll(strings.ReplaceAll(kClean, "__", "_"), "-", "_")
			if kCleanNorm == cleanNameNorm || strings.HasSuffix(kCleanNorm, "_"+cleanNameNorm) {
				return canon, true
			}
		}

		// AGY call_mcp_tool fallback: if client has call_mcp_tool and incoming is an MCP tool
		if strings.HasPrefix(lower, "mcp__") || strings.HasPrefix(lower, "mcp_") {
			if d, ok := findToolDef(by, "call_mcp_tool"); ok {
				return d.Name, true
			}
		}

		return "", false
	}

	var out []types.ToolCall
	seen := map[string]bool{}
	add := func(name, id, args string) {
		canon, ok := canonical(name)
		if !ok {
			// If incoming is call_mcp_tool, inspect args to map to client's mcp__server__tool
			if strings.EqualFold(name, "call_mcp_tool") {
				var mcpArgs struct {
					ServerName string          `json:"ServerName"`
					ToolName   string          `json:"ToolName"`
					Arguments  json.RawMessage `json:"Arguments"`
				}
				if json.Unmarshal([]byte(args), &mcpArgs) == nil && mcpArgs.ServerName != "" && mcpArgs.ToolName != "" {
					candidate := "mcp__" + mcpArgs.ServerName + "__" + mcpArgs.ToolName
					if c, found := canonical(candidate); found {
						canon = c
						ok = true
						if len(mcpArgs.Arguments) > 0 && string(mcpArgs.Arguments) != "null" {
							args = string(mcpArgs.Arguments)
						}
					}
				}
			}
			if !ok {
				return
			}
		}

		// If client expects call_mcp_tool and incoming is an mcp__server__tool name:
		if strings.EqualFold(canon, "call_mcp_tool") && (strings.HasPrefix(strings.ToLower(name), "mcp__") || strings.HasPrefix(strings.ToLower(name), "mcp_")) {
			clean := strings.TrimPrefix(strings.ToLower(name), "mcp__")
			clean = strings.TrimPrefix(clean, "mcp_")
			parts := strings.SplitN(clean, "__", 2)
			if len(parts) < 2 {
				parts = strings.SplitN(clean, "_", 2)
			}
			if len(parts) == 2 {
				var innerArgs any
				if json.Unmarshal([]byte(args), &innerArgs) == nil {
					wrapped := map[string]any{
						"ServerName":  parts[0],
						"ToolName":    parts[1],
						"Arguments":   innerArgs,
						"toolAction":  "Calling MCP tool",
						"toolSummary": "MCP tool call",
					}
					if b, err := json.Marshal(wrapped); err == nil {
						args = string(b)
					}
				}
			}
		}

		args = strings.TrimSpace(args)
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			b, _ := json.Marshal(map[string]string{"command": args})
			args = string(b)
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
	return NormalizeToolCalls(calls, defs, "")
}

// coerceToolArgs remaps common aliases (path↔file_path, cmd↔command, content↔CodeContent,
// old↔new strings, grep/find queries) to the keys the client dialect schema expects.
func coerceToolArgs(argsJSON string, def types.ToolDef) string {
	var m map[string]any
	if json.Unmarshal([]byte(argsJSON), &m) != nil || len(m) == 0 {
		return argsJSON
	}
	changed := false
	remap := func(want string, alts ...string) {
		if want == "" {
			return
		}
		if _, has := m[want]; has {
			return
		}
		for _, alt := range alts {
			if v, ok := m[alt]; ok {
				m[want] = v
				delete(m, alt)
				changed = true
				return
			}
		}
	}

	wantPath := toolArgKey(def, "file_path", "path", "AbsolutePath", "TargetFile", "SearchPath", "SearchDirectory")
	remap(wantPath, "file_path", "path", "AbsolutePath", "TargetFile", "SearchPath", "SearchDirectory", "file", "filename", "filepath")

	wantCmd := toolArgKey(def, "command", "CommandLine", "cmd")
	remap(wantCmd, "command", "CommandLine", "cmd", "script", "code")

	wantContent := toolArgKey(def, "content", "CodeContent", "contents", "text")
	remap(wantContent, "content", "CodeContent", "contents", "text", "body", "code")

	wantOld := toolArgKey(def, "old_string", "TargetContent", "old_str", "old")
	remap(wantOld, "old_string", "TargetContent", "old_str", "old", "orig", "original")

	wantNew := toolArgKey(def, "new_string", "ReplacementContent", "new_str", "new")
	remap(wantNew, "new_string", "ReplacementContent", "new_str", "new", "replacement")

	wantQuery := toolArgKey(def, "query", "pattern", "Query", "Pattern")
	remap(wantQuery, "query", "pattern", "Query", "Pattern", "regex", "search_term")

	wantDir := toolArgKey(def, "dir", "directory", "SearchDirectory", "SearchPath")
	remap(wantDir, "dir", "directory", "SearchDirectory", "SearchPath", "path", "cwd")

	wantSkill := toolArgKey(def, "skill", "skill_name", "name")
	remap(wantSkill, "skill", "skill_name", "name", "skillName")

	// Ensure required schema parameters for AGY / strict client tools if missing
	schemaKeysList := schemaKeys(def.InputSchema, 20)
	hasKey := func(k string) bool {
		for _, sk := range schemaKeysList {
			if sk == k {
				return true
			}
		}
		return false
	}

	// Unwrap single-item array strings for path/cmd if web model generated an array
	for _, k := range []string{"file_path", "path", "AbsolutePath", "TargetFile", "SearchPath", "command", "CommandLine"} {
		if arr, ok := m[k].([]any); ok && len(arr) > 0 {
			if firstStr, isStr := arr[0].(string); isStr {
				m[k] = firstStr
				changed = true
			}
		}
	}

	if hasKey("Overwrite") {
		if v, ok := m["Overwrite"]; !ok || v == nil {
			m["Overwrite"] = true
			changed = true
		} else if s, isStr := v.(string); isStr {
			m["Overwrite"] = strings.EqualFold(s, "true") || s == "1"
			changed = true
		}
	}
	if hasKey("AllowMultiple") {
		if v, ok := m["AllowMultiple"]; !ok || v == nil {
			m["AllowMultiple"] = false
			changed = true
		} else if s, isStr := v.(string); isStr {
			m["AllowMultiple"] = strings.EqualFold(s, "true") || s == "1"
			changed = true
		}
	}
	if hasKey("Cwd") {
		if _, ok := m["Cwd"]; !ok {
			m["Cwd"] = "."
			changed = true
		}
	}
	if hasKey("WaitMsBeforeAsync") {
		if v, ok := m["WaitMsBeforeAsync"]; !ok || v == nil {
			m["WaitMsBeforeAsync"] = 10000
			changed = true
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["WaitMsBeforeAsync"] = n
				changed = true
			}
		}
	}
	if hasKey("toolAction") {
		if _, ok := m["toolAction"]; !ok {
			m["toolAction"] = "Running tool"
			changed = true
		}
	}
	if hasKey("toolSummary") {
		if _, ok := m["toolSummary"]; !ok {
			m["toolSummary"] = "Tool execution"
			changed = true
		}
	}
	if hasKey("Instruction") {
		if _, ok := m["Instruction"]; !ok {
			m["Instruction"] = "Apply modification"
			changed = true
		}
	}
	if hasKey("Description") {
		if _, ok := m["Description"]; !ok {
			m["Description"] = "Code change"
			changed = true
		}
	}
	if hasKey("StartLine") {
		if v, ok := m["StartLine"]; !ok || v == nil {
			m["StartLine"] = 1
			changed = true
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["StartLine"] = n
				changed = true
			}
		}
	}
	if hasKey("EndLine") {
		if v, ok := m["EndLine"]; !ok || v == nil {
			m["EndLine"] = 1000000
			changed = true
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["EndLine"] = n
				changed = true
			}
		}
	}

	// Subagent coercion:
	// AGY client expects invoke_subagent with Subagents array
	if strings.EqualFold(def.Name, "invoke_subagent") {
		if _, hasSub := m["Subagents"]; !hasSub {
			promptVal := ""
			roleVal := "Codebase Researcher"
			typeNameVal := "research"
			if p, ok := m["prompt"].(string); ok && p != "" {
				promptVal = p
			} else if t, ok := m["task"].(string); ok && t != "" {
				promptVal = t
			} else if d, ok := m["description"].(string); ok && d != "" {
				promptVal = d
			}
			if promptVal != "" {
				if r, ok := m["description"].(string); ok && r != "" && r != promptVal {
					roleVal = r
				}
				if tn, ok := m["type"].(string); ok && tn != "" {
					typeNameVal = tn
				} else if tn, ok := m["TypeName"].(string); ok && tn != "" {
					typeNameVal = tn
				}
				m["Subagents"] = []map[string]any{
					{
						"TypeName":  typeNameVal,
						"Role":      roleVal,
						"Prompt":    promptVal,
						"Model":     "inherit",
						"Workspace": "inherit",
					},
				}
				delete(m, "prompt")
				delete(m, "description")
				delete(m, "task")
				changed = true
			}
		}
	} else if strings.EqualFold(def.Name, "agent") || strings.EqualFold(def.Name, "subagent") || strings.EqualFold(def.Name, "task") {
		// Claude/Codex client expects Agent with prompt/description
		if subs, ok := m["Subagents"].([]any); ok && len(subs) > 0 {
			if first, ok := subs[0].(map[string]any); ok {
				if p, ok := first["Prompt"].(string); ok && p != "" {
					m["prompt"] = p
				}
				if r, ok := first["Role"].(string); ok && r != "" {
					m["description"] = r
				}
				delete(m, "Subagents")
				delete(m, "toolAction")
				delete(m, "toolSummary")
				changed = true
			}
		}
	}

	if !changed {
		return argsJSON
	}
	b, err := json.Marshal(m)
	if err != nil {
		return argsJSON
	}
	return string(b)
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
