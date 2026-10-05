package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/tools/jsonrepair"
	"amux-accounts/pkg/types"
)

// Protocol is tiny on purpose: catalog is rebuilt every request from the
// client's live tools[] (new MCP / plugin / Skill appear with no code change).
// Keep preamble short — every tools turn pays this cost on cold-start threads.
const webToolPreamble = `Coding-agent backend. Client runs tools on the real repo. [Tool result] = verified CLI output.
Rules: need file/cmd → emit <tool_call> now; never claim lack of tools / ask to paste / fake edits.
Format:
<thought>
Reasoning / thinking step (optional)
</thought>
<tool_call>
{"name":"TOOL","arguments":{...}}
</tool_call>
Example:
User: check git status
Assistant: <tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>

Multiple blocks OK. CATALOG
`

const webToolCloser = `
[end] Need data → <tool_call> now. Have tool results → answer fully. No checklist / paste / "no tools".
`

var (
	reThought        = regexp.MustCompile(`(?s)<thought>\s*(.*?)\s*</thought>`)
	reXMLTool        = regexp.MustCompile(`(?s)<tool_call(?:\s+name="?([^"\s>]+)"?)?(?:\s+id="?([^"\s>]+)"?)?[^>]*>\s*(.*?)\s*</tool_call>`)
	reInvokeTool     = regexp.MustCompile(`(?s)<(?:invoke|function_call)\s+name="?([^"\s>]+)"?(?:\s+id="?([^"\s>]+)"?)?[^>]*>\s*(.*?)\s*</(?:invoke|function_call)>`)
	reXMLParam       = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)">\s*(.*?)\s*</parameter>`)
	reAMUXTool       = regexp.MustCompile(`(?s)<<<AMUX_TOOL\s+name="([^"]+)"(?:\s+id="([^"]*)")?\s*>>>\s*(.*?)\s*<<<END_AMUX_TOOL>>>`)
	reToolJSON       = regexp.MustCompile("(?s)```(?:tool_call|json)\\s*\n(\\{[\\s\\S]*?\\})\\s*```")
	reBashFence      = regexp.MustCompile("(?s)```(?:bash|sh|zsh|shell)\\s*\n(.*?)\\s*```")
	// ChatGPT copies Claude Code's display form: [tool_call name=Bash id=…] or history format [Tool call: Bash id=…]
	reBracketTool    = regexp.MustCompile(`(?is)\[(?:tool[ _]call|tool_call):?\s+(?:name="?)?([A-Za-z0-9_-]+)"?(?:\s+id="?([^"\s\]]+)"?)?\]\s*(\{[\s\S]*?\})`)
	reBracketToolAlt = regexp.MustCompile(`(?is)\[(?:tool[ _]call|tool_call):?\s+([A-Za-z0-9_-]+)\s*(\{[\s\S]*?\})\]`)
	reTrailComma     = regexp.MustCompile(`,\s*([}\]])`)
)

// WebPreamble is appended to a web-backend prompt when the client sent tools[].
func WebPreamble(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	return webToolPreamble + catalogBlock(defs)
}

// WebPreambleForRequest returns the appropriate preamble for a request with tools.
func WebPreambleForRequest(req *types.ChatRequest) string {
	if req == nil || len(req.Tools) == 0 {
		return ""
	}
	return WebPreamble(req.Tools)
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
	keys := schemaKeyTypes(d.InputSchema, 24)
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
		hasStreamedThinking := false
		for ch := range inner {
			if ch.ID != "" {
				id = ch.ID
			}
			if ch.Error != nil {
				out <- ch
				return
			}
			if ch.Thinking != "" {
				hasStreamedThinking = true
				out <- types.StreamChunk{ID: id, Thinking: ch.Thinking}
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
		if !hasStreamedThinking {
			for _, m := range reThought.FindAllStringSubmatch(text, -1) {
				if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
					out <- types.StreamChunk{ID: id, Thinking: strings.TrimSpace(m[1])}
				}
			}
		}
		calls, forced := FinalizeWebToolCalls(text, defs, hist)
		logWebTools(source, calls, text)
		if len(calls) == 0 {
			cleanText := text
			if hasExplicitWebToolMarkup(text) || strings.Contains(text, "<thought") {
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

// FinalizeWebToolCalls parses web text into tool calls and coerces arg keys to the client dialect schema.
func FinalizeWebToolCalls(text string, defs []types.ToolDef, hist []types.ChatMessage) (calls []types.ToolCall, forced bool) {
	if strings.TrimSpace(text) == "" || len(defs) == 0 {
		return nil, false
	}
	// When prior tool history exists or explicit markup is present, avoid interpreting plain markdown bash codeblocks as tool executions.
	allowBashFence := !historyHasTools(hist) && !hasExplicitWebToolMarkup(text)
	calls = parseWebTools(text, defs, allowBashFence)
	return coerceAllToolArgs(calls, defs), false
}

func historyHasTools(hist []types.ChatMessage) bool {
	for _, m := range hist {
		if strings.EqualFold(m.Role, "tool") || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func hasExplicitWebToolMarkup(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "[tool_call") ||
		strings.Contains(lower, "[tool call") ||
		strings.Contains(lower, "<invoke") ||
		strings.Contains(lower, "<<<amux_tool") ||
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

func findToolDef(by map[string]types.ToolDef, names ...string) (types.ToolDef, bool) {
	for _, n := range names {
		if d, ok := by[strings.ToLower(n)]; ok {
			return d, true
		}
	}
	return types.ToolDef{}, false
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
		attrName, attrID, body := m[1], m[2], m[3]
		if name, id, args, ok := parseToolCallJSON(body); ok {
			if attrName != "" && name == "" {
				name = attrName
			}
			if attrID != "" && id == "" {
				id = attrID
			}
			add(name, id, args)
		} else if attrName != "" {
			add(attrName, attrID, body)
		}
	}
	for _, m := range reInvokeTool.FindAllStringSubmatch(text, -1) {
		name, id, body := m[1], m[2], strings.TrimSpace(m[3])
		params := reXMLParam.FindAllStringSubmatch(body, -1)
		if len(params) > 0 {
			paramMap := make(map[string]any)
			for _, p := range params {
				pName := p[1]
				pVal := strings.TrimSpace(p[2])
				var parsed any
				if json.Unmarshal([]byte(pVal), &parsed) == nil {
					paramMap[pName] = parsed
				} else {
					paramMap[pName] = pVal
				}
			}
			if b, err := json.Marshal(paramMap); err == nil {
				add(name, id, string(b))
				continue
			}
		}
		if n, i, args, ok := parseToolCallJSON(body); ok {
			if n != "" {
				name = n
			}
			if i != "" {
				id = i
			}
			add(name, id, args)
			continue
		}
		if body != "" {
			add(name, id, body)
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
	for _, m := range reBracketToolAlt.FindAllStringSubmatch(text, -1) {
		name, raw := m[1], strings.TrimSpace(m[2])
		if n, i, args, ok := parseToolCallJSON(raw); ok {
			if n != "" {
				name = n
			}
			add(name, i, args)
			continue
		}
		add(name, "", raw)
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
		if v, ok := m["Instruction"]; !ok || v == nil || v == "" {
			m["Instruction"] = "Apply modifications"
			changed = true
		}
	}
	if hasKey("Description") {
		if v, ok := m["Description"]; !ok || v == nil || v == "" {
			m["Description"] = "Code change"
			changed = true
		}
	}
	// Resolve relative paths to absolute for strict tools (e.g. AGY view_file)
	if hasKey("AbsolutePath") {
		if p, ok := m["AbsolutePath"].(string); ok && p != "" && !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				m["AbsolutePath"] = abs
				changed = true
			}
		}
	}

	// Inspect file on disk to determine actual line bounds and TargetContent location
	var fileLines int
	var foundStart, foundEnd int
	checkPath := ""
	if p, ok := m["TargetFile"].(string); ok && p != "" {
		checkPath = p
	} else if p, ok := m["AbsolutePath"].(string); ok && p != "" {
		checkPath = p
	}
	if checkPath != "" {
		if contentBytes, err := os.ReadFile(checkPath); err == nil {
			lines := strings.Split(string(contentBytes), "\n")
			fileLines = len(lines)
			if targetStr, ok := m["TargetContent"].(string); ok && targetStr != "" {
				targetLines := strings.Split(targetStr, "\n")
				tLen := len(targetLines)
				for i := 0; i <= len(lines)-tLen; i++ {
					match := true
					for j := 0; j < tLen; j++ {
						if lines[i+j] != targetLines[j] {
							match = false
							break
						}
					}
					if match {
						foundStart = i + 1
						foundEnd = i + tLen
						break
					}
				}
			}
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
		if foundStart > 0 {
			m["StartLine"] = foundStart
			changed = true
		} else if v, ok := m["StartLine"]; !ok || v == nil {
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
		if foundEnd > 0 {
			m["EndLine"] = foundEnd
			changed = true
		} else if v, ok := m["EndLine"]; !ok || v == nil {
			if fileLines > 0 {
				m["EndLine"] = fileLines
			} else {
				m["EndLine"] = 1000
			}
			changed = true
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["EndLine"] = n
				changed = true
			}
		}
		if fileLines > 0 {
			if curEnd, ok := m["EndLine"].(int); ok && curEnd > fileLines {
				m["EndLine"] = fileLines
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
	return jsonrepair.Repair(s)
}

func parseToolCallJSON(raw string) (name, id, args string, ok bool) {
	raw = jsonrepair.StripMarkdownFences(raw)
	raw = strings.TrimSpace(raw)
	raw = reTrailComma.ReplaceAllString(raw, "$1")
	var probe struct {
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
	}
	if json.Unmarshal([]byte(raw), &probe) != nil || probe.Name == "" {
		repaired := jsonrepair.Repair(raw)
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
				// 1. Try "command" / "CommandLine" / "cmd"
				reCmd := regexp.MustCompile(`(?s)"(?:command|CommandLine|cmd)"\s*:\s*"(.*)"\s*\}*\s*\}*$`)
				if mcmd := reCmd.FindStringSubmatch(raw); len(mcmd) > 1 {
					cmd := mcmd[1]
					b, _ := json.Marshal(map[string]string{"command": cmd})
					return name, id, string(b), true
				}
				// 2. Try "file_path" / "path" / "AbsolutePath" / "TargetFile"
				rePath := regexp.MustCompile(`(?s)"(?:file_path|path|AbsolutePath|TargetFile)"\s*:\s*"(.*?)"`)
				if mpath := rePath.FindStringSubmatch(raw); len(mpath) > 1 {
					filePath := mpath[1]
					payload := map[string]string{"file_path": filePath}
					reContent := regexp.MustCompile(`(?s)"(?:content|CodeContent|new_string|ReplacementContent)"\s*:\s*"(.*)"\s*\}*\s*\}*$`)
					if mcont := reContent.FindStringSubmatch(raw); len(mcont) > 1 {
						payload["content"] = mcont[1]
					}
					b, _ := json.Marshal(payload)
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

// StripWebToolMarkup removes protocol / bash fences / thought tags so Claude Code does not
// also print the commands or thinking internal steps as raw assistant text.
func StripWebToolMarkup(text string) string {
	s := reThought.ReplaceAllString(text, "")
	s = reXMLTool.ReplaceAllString(s, "")
	s = reInvokeTool.ReplaceAllString(s, "")
	s = reAMUXTool.ReplaceAllString(s, "")
	s = reBracketTool.ReplaceAllString(s, "")
	s = reBracketToolAlt.ReplaceAllString(s, "")
	s = reToolJSON.ReplaceAllString(s, "")
	s = reBashFence.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
