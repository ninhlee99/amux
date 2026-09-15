package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/types"
)

// Protocol is tiny on purpose: catalog is rebuilt every request from the
// client's live tools[] (new MCP / plugin / Skill appear with no code change).
// Keep preamble short — every tools turn pays this cost on cold-start threads.
const webToolPreamble = `Coding-agent backend. Client runs tools on the real repo. [Tool result] = verified CLI output.
Rules: need file/cmd → emit <tool_call> now; never claim lack of tools / ask to paste / fake edits.
Format:
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
	reWebBashCmd   = regexp.MustCompile("(?i)(?:`((?:git|ls|find|grep|am)\\s+[^`\\n]+)`|\\b(git\\s+(?:diff|status|log|show|branch|grep|rev-parse)(?:\\s+(?:--?[a-zA-Z0-9_.-]+|[a-zA-Z0-9_./-]+))*|ls(?:\\s+-[a-zA-Z0-9]+)?)\\b)")
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
	var b strings.Builder
	for _, d := range defs {
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
				logWebTools(source, ch.ToolCalls, ch.LogText)
				out <- ch
				for rest := range inner {
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
	if strings.TrimSpace(text) == "" || isWebTitleJSON(text) {
		return false
	}
	var h []types.ChatMessage
	if len(hist) > 0 {
		h = hist[0]
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
	if isWebToolRefusal(text) || isWebWorkIncomplete(text) {
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
		if isWebToolRefusal(text) || isWebWorkIncomplete(text) {
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
	case strings.HasPrefix(low, "ls"):
		return regexp.MustCompile(`(?i)^ls(?:\s+-[a-zA-Z0-9]+)*\s*$`).MatchString(cmd)
	case strings.HasPrefix(low, "am "):
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
		}
		return "", false
	}

	var out []types.ToolCall
	seen := map[string]bool{}
	add := func(name, id, args string) {
		canon, ok := canonical(name)
		if !ok {
			return
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
		var probe struct {
			Name      string          `json:"name"`
			ID        string          `json:"id"`
			Arguments json.RawMessage `json:"arguments"`
			Input     json.RawMessage `json:"input"`
		}
		if json.Unmarshal([]byte(m[1]), &probe) != nil || probe.Name == "" {
			continue
		}
		args := probe.Arguments
		if len(args) == 0 {
			args = probe.Input
		}
		add(probe.Name, probe.ID, string(args))
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
	by := map[string]types.ToolDef{}
	for _, d := range defs {
		by[strings.ToLower(d.Name)] = d
	}
	for i := range calls {
		if d, ok := by[strings.ToLower(calls[i].Name)]; ok {
			calls[i].Arguments = coerceToolArgs(calls[i].Arguments, d)
		}
	}
	return calls
}

// coerceToolArgs remaps common aliases (path↔file_path, cmd↔command) to the
// keys the client dialect schema expects.
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
	wantPath := toolArgKey(def, "file_path", "path", "AbsolutePath")
	remap(wantPath, "file_path", "path", "AbsolutePath", "file", "filename")
	wantCmd := toolArgKey(def, "command", "CommandLine", "cmd")
	remap(wantCmd, "command", "CommandLine", "cmd", "script", "code")
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
				reCmd := regexp.MustCompile(`(?s)"command"\s*:\s*"(.*?)"\s*\}*\s*\}*$`)
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
