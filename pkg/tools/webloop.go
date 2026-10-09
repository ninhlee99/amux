package tools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/tools/jsonrepair"
	"amux-accounts/pkg/types"
)

// Claude Web templates: clean, direct agent instructions without mentioning external container sandboxes.
const webToolPreambleClaude = `In this environment you have access to workspace tools.
The client executes your tool calls directly in the workspace and returns [Tool result].
To invoke a tool, emit a <tool_call> block:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Wait for [Tool result] before continuing.

Example:
User: check git status
Assistant: <tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>

Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG
`

const webToolReminderClaude = `To inspect files, run terminal commands, or edit code on the repository, invoke tools using <tool_call>:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
The client executes tool calls directly in the workspace and replies with [Tool result].
Example:
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>
`

const webToolCloserClaude = `
To inspect files, edit code, run tests, commit changes, or invoke available tools/MCP, emit <tool_call> now.
`

// ChatGPT Web templates: clean, direct agent instructions without synthetic markers.
const webToolPreambleChatGPT = `You are the reasoning engine of a text-based coding agent.
You do not need native tools or function calling: the agent client reads your reply as plain text,
runs every <tool_call> block it finds on the user's machine, and sends the output back as [Tool result].
Writing a <tool_call> block is therefore always possible — it is just text in your reply. Never say the tools are unavailable.
(Do not use your own python/container sandbox; it cannot see the user's workspace.)
Format:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Then stop and wait for [Tool result].

Example:
User: check git status
Assistant: <tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>

Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG (tool names the client runs, with argument keys)
`

const webToolReminderChatGPT = `TOOLS LIVE: To inspect files, run terminal commands, or edit code on the repository, write a <tool_call> block — the client runs it, no native tools needed:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Built-in python/container tools cannot access this workspace — use <tool_call>, then await [Tool result].
The client executes tool calls directly in the workspace and replies with [Tool result].
Example:
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>
`

const webToolCloserChatGPT = `
[end] To inspect, edit, write, test, commit on the repository, or invoke available tools/MCP/skills, write the <tool_call> block now (the client executes it; you need no native tools). Only provide final answer when all requested tasks are completely finished.
`

// Gemini Web templates: streamlined for Google Gemini StreamGenerate without synthetic markers.
const webToolPreambleGemini = `In this environment you have access to workspace tools.
The client executes your tool calls directly in the workspace and returns [Tool result].
To inspect files, execute terminal commands, or edit code in the repository, output tool invocations using <tool_call> blocks:
<thought>
Reasoning / thinking step (optional)
</thought>
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Wait for [Tool result] before continuing.

Example:
User: check git status
Assistant: <tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>

Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG
`

const webToolReminderGemini = `To inspect files, run terminal commands, or edit code on the repository, invoke tools using <tool_call>:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
The client executes tool calls directly in the workspace and replies with [Tool result].
Example:
<tool_call>
{"name":"Bash","arguments":{"command":"git status"}}
</tool_call>
`

const webToolCloserGemini = `
To inspect, edit, write, test, commit on the repository, or invoke available tools/MCP, emit <tool_call> now. Only provide final answer when all requested tasks are completely finished.
`

const webToolPreamble = webToolPreambleChatGPT
const webToolReminder = webToolReminderChatGPT
const webToolCloser = webToolCloserChatGPT

var (
	reThought       = regexp.MustCompile(`(?si)<thought>\s*(.*?)\s*</thought>`)
	reThinking      = regexp.MustCompile(`(?si)<thinking>\s*(.*?)\s*</thinking>`)
	reReflection    = regexp.MustCompile(`(?si)<reflection>\s*(.*?)\s*</reflection>`)
	reXMLTool       = regexp.MustCompile(`(?si)<tool_call(?:\s+name="?([^"\s>]+)"?)?(?:\s+id="?([^"\s>]+)"?)?[^>]*>\s*(.*?)\s*</tool_call>`)
	reHyphenTool    = regexp.MustCompile(`(?si)<tool-call(?:\s+name="?([^"\s>]+)"?)?(?:\s+id="?([^"\s>]+)"?)?[^>]*>\s*(.*?)\s*</tool-call>`)
	reInvokeTool    = regexp.MustCompile(`(?si)<(?:invoke|function_call)(?:\s+name="?([^"\s>]+)"?)?(?:\s+id="?([^"\s>]+)"?)?[^>]*>\s*(.*?)\s*</(?:invoke|function_call)>`)
	reXMLParam      = regexp.MustCompile(`(?si)<parameter\s+name="([^"]+)">\s*(.*?)\s*</parameter>`)
	reAMUXTool      = regexp.MustCompile(`(?si)<<<AMUX_TOOL\s+name="([^"]+)"(?:\s+id="([^"]*)")?\s*>>>\s*(.*?)\s*<<<END_AMUX_TOOL>>>`)
	reToolJSON      = regexp.MustCompile("(?si)```(?:tool_call|json|tool)?\\s*\\n?\\s*(\\{[\\s\\S]*?\\})\\s*```")
	reBashFence     = regexp.MustCompile("(?si)```(?:bash|sh|zsh|shell)\\s*\n(.*?)\\s*```")
	reBareJSONKey   = regexp.MustCompile(`([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)\s*:`)
	reGeminiCall    = regexp.MustCompile(`(?si)\b(?:call:(?:default_api:)?([A-Za-z0-9_-]+))\s*(\{[\s\S]*?\}|\([^\n)]*\))`)
	reActionTool    = regexp.MustCompile(`(?im)^Action:\s*([A-Za-z0-9_-]+)\s*\n(?:Action\s+Input|Input|Arguments|Args):\s*(\{[\s\S]*?\}|"[^"\n]*"|[^\n]+)`)
	reToolCallFence = regexp.MustCompile("(?si)```(?:tool_call|tool)\\s*\\n?[\\s\\S]*?```")
	// ChatGPT copies Claude Code's display form: [tool_call name=Bash id=…] or history format [Tool call: Bash id=…]
	reBracketTool      = regexp.MustCompile(`(?is)\[(?:tool[ _]call|tool_call):?\s+(?:name="?)?([A-Za-z0-9_-]+)"?(?:\s+id="?([^"\s\]]+)"?)?\]\s*(\{[\s\S]*?\})`)
	reBracketToolAlt   = regexp.MustCompile(`(?is)\[(?:tool[ _]call|tool_call):?\s+([A-Za-z0-9_-]+)\s*(\{[\s\S]*?\})\]`)
	reStrayBracketTool = regexp.MustCompile(`(?is)\[(?:tool[ _]call|tool_call)[^\]]*\]`)
	reEndNotice        = regexp.MustCompile(`(?im)^\[end\]\b[^\n]*\n?`)
	// Gemini echoes protocol lines of the preamble as its own prose.
	reProtocolEcho     = regexp.MustCompile(`(?im)^[ \t]*(?:Wait for \[Tool result\] before continuing\.?|Multiple blocks OK\.?|Several blocks only for independent calls[^\n]*|Then stop and wait for \[Tool result\]\.?)[ \t]*\n?`)
	reXferNotice       = regexp.MustCompile(`(?im)^\[xfer\]\s+Continue[^\n]*\n?`)
	reCatalogNotice    = regexp.MustCompile(`(?im)^CATALOG\b[^\n]*\n?`)
	reToolsLiveNotice  = regexp.MustCompile(`(?im)^TOOLS LIVE:[^\n]*\n?`)
	reToolResultMarker = regexp.MustCompile(`(?im)^\[Tool result[^\n]*\]:?\n?`)
	reTrailComma       = regexp.MustCompile(`,\s*([}\]])`)
	reEmptyFence       = regexp.MustCompile("(?si)```[a-zA-Z0-9_-]*\\s*```")
	reSystemReminder   = regexp.MustCompile(`(?si)<system-reminder>.*?</system-reminder>`)
)

// CleanUserTurnContent strips IDE metadata such as <system-reminder> blocks and normalizes "(no content)".
// Returns empty string if no substantive user content remains.
func CleanUserTurnContent(content string) string {
	cleaned := reSystemReminder.ReplaceAllString(content, "")
	cleaned = strings.TrimSpace(cleaned)
	if strings.EqualFold(cleaned, "(no content)") {
		return ""
	}
	return cleaned
}

// ExtractThoughts extracts all content inside <thought>, <thinking>, or <reflection> tags.
func ExtractThoughts(text string) string {
	var sb strings.Builder
	for _, m := range reThought.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(strings.TrimSpace(m[1]))
		}
	}
	for _, m := range reThinking.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(strings.TrimSpace(m[1]))
		}
	}
	for _, m := range reReflection.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(strings.TrimSpace(m[1]))
		}
	}
	return sb.String()
}

// DiscoverKnownMCPServers scans known MCP server locations (AGY, Cursor, Claude, Windsurf, VS Code)
// to return known server names sorted by length descending so longer prefixes match first.
func DiscoverKnownMCPServers() []string {
	seen := map[string]bool{}
	var list []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			list = append(list, s)
		}
	}

	parseJSONFile := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return
		}
		var root struct {
			MCPServers map[string]any `json:"mcpServers"`
			Servers    map[string]any `json:"servers"`
			Projects   map[string]struct {
				MCPServers map[string]any `json:"mcpServers"`
			} `json:"projects"`
		}
		if json.Unmarshal(data, &root) == nil {
			for k := range root.MCPServers {
				add(k)
			}
			for k := range root.Servers {
				add(k)
			}
			for _, p := range root.Projects {
				for k := range p.MCPServers {
					add(k)
				}
			}
		}
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		// Antigravity MCP directory
		mcpDir := filepath.Join(home, ".gemini", "antigravity-cli", "mcp")
		if entries, err := os.ReadDir(mcpDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					add(e.Name())
				}
			}
		}

		// IDE configuration files
		parseJSONFile(filepath.Join(home, ".cursor", "mcp.json"))
		parseJSONFile(filepath.Join(home, ".config", "cursor", "mcp.json"))
		parseJSONFile(filepath.Join(home, ".claude.json"))
		parseJSONFile(filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"))
		parseJSONFile(filepath.Join(home, ".config", "claude", "claude_desktop_config.json"))
		parseJSONFile(filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"))
		parseJSONFile(filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"))
		parseJSONFile(filepath.Join(home, ".config", "Code", "User", "mcp.json"))
		parseJSONFile(filepath.Join(home, ".gemini", "settings.json"))
		parseJSONFile(filepath.Join(home, ".gemini", "config", "mcp_config.json"))
		parseJSONFile(filepath.Join(home, ".amux", "mcp.json"))
		parseJSONFile(filepath.Join(home, ".amux", "config.json"))
	}
	add("amux")

	sort.Slice(list, func(i, j int) bool {
		return len(list[i]) > len(list[j])
	})
	return list
}

// SplitMCPServerTool decomposes an MCP tool name like "mcp__server__tool" or
// "mcp_supabase_mcp_server_list_tables" into (ServerName, ToolName).
func SplitMCPServerTool(raw string) (string, string) {
	clean := raw
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "mcp__") {
		clean = clean[5:]
	} else if strings.HasPrefix(lower, "mcp_") {
		clean = clean[4:]
	} else if strings.HasPrefix(lower, "mcp.") {
		clean = clean[4:]
	}
	known := DiscoverKnownMCPServers()
	if parts := strings.SplitN(clean, "__", 2); len(parts) == 2 {
		for _, s := range known {
			if strings.EqualFold(s, parts[0]) {
				return s, parts[1]
			}
		}
		return parts[0], parts[1]
	}

	lowerClean := strings.ToLower(clean)
	for _, s := range known {
		sNorm := strings.ToLower(strings.ReplaceAll(s, "-", "_"))
		if strings.HasPrefix(lowerClean, sNorm+"_") {
			return s, clean[len(sNorm)+1:]
		}
		if strings.HasPrefix(lowerClean, strings.ToLower(s)+"_") {
			return s, clean[len(s)+1:]
		}
	}

	if parts := strings.SplitN(clean, "_", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", clean
}

// NormalizeWebProvider normalizes a provider string to "claude", "chatgpt", or "gemini".
func NormalizeWebProvider(provider string) string {
	lower := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(lower, "claude"):
		return "claude"
	case strings.Contains(lower, "gemini"):
		return "gemini"
	default:
		return "chatgpt"
	}
}

func hasBashTool(defs []types.ToolDef) bool {
	for _, d := range defs {
		l := strings.ToLower(d.Name)
		if l == "bash" || l == "run_command" || l == "run_terminal_command" || l == "exec_command" || l == "shell" {
			return true
		}
	}
	return false
}

func sampleArgumentsForDef(d types.ToolDef) string {
	if len(d.InputSchema) == 0 {
		return "{}"
	}
	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(d.InputSchema, &s) != nil || len(s.Properties) == 0 {
		return "{}"
	}
	sample := make(map[string]any)
	count := 0
	for k, prop := range s.Properties {
		if count >= 2 {
			break
		}
		switch prop.Type {
		case "string":
			sample[k] = "sample_" + k
		case "integer", "number":
			sample[k] = 1
		case "boolean":
			sample[k] = true
		default:
			sample[k] = "sample_" + k
		}
		count++
	}
	b, err := json.Marshal(sample)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func dynamicToolExample(defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	if hasBashTool(defs) {
		return "Example:\nUser: check git status\nAssistant: <tool_call>\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"git status\"}}\n</tool_call>\n\n"
	}
	d := defs[0]
	return fmt.Sprintf("Example:\nAssistant: <tool_call>\n{\"name\":%q,\"arguments\":%s}\n</tool_call>\n\n", d.Name, sampleArgumentsForDef(d))
}

// WebPreambleForProvider returns the provider-tailored preamble for requests with tools.
func WebPreambleForProvider(provider string, defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	if hasBashTool(defs) {
		switch NormalizeWebProvider(provider) {
		case "claude":
			return webToolPreambleClaude + catalogBlock(defs)
		case "gemini":
			return webToolPreambleGemini + catalogBlock(defs)
		default:
			return webToolPreambleChatGPT + catalogBlock(defs)
		}
	}

	example := dynamicToolExample(defs)
	switch NormalizeWebProvider(provider) {
	case "claude":
		return `In this environment you have access to workspace tools.
The client executes your tool calls directly in the workspace and returns [Tool result].
To invoke a tool, emit a <tool_call> block:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Wait for [Tool result] before continuing.

` + example + `Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG
` + catalogBlock(defs)
	case "gemini":
		return `In this environment you have access to workspace tools.
The client executes your tool calls directly in the workspace and returns [Tool result].
To inspect files, execute terminal commands, or edit code in the repository, output tool invocations using <tool_call> blocks:
<thought>
Reasoning / thinking step (optional)
</thought>
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Wait for [Tool result] before continuing.

` + example + `Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG
` + catalogBlock(defs)
	default:
		return `You are the reasoning engine of a text-based agent.
You do not need native tools: the client reads your reply as plain text, runs every <tool_call> block it finds, and returns [Tool result].
Writing a <tool_call> block is always possible. Never say the tools are unavailable.
(Do not use your own python/container sandbox for workspace files; use <tool_call>).
To invoke a tool, emit a <tool_call> block:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Wait for [Tool result] before continuing.

` + example + `Several blocks only for independent calls (at most 5); a call that needs another's output waits for its [Tool result]. Never write results, ids or conclusions before the [Tool result] arrives. Arguments marked ? are optional — omit them unless you need them.
CATALOG
` + catalogBlock(defs)
	}
}

// WebPreamble is appended to a web-backend prompt when the client sent tools[].
func WebPreamble(defs []types.ToolDef) string {
	return WebPreambleForProvider("", defs)
}

// WebPreambleForRequest returns the appropriate preamble for a request with tools.
func WebPreambleForRequest(req *types.ChatRequest) string {
	if req == nil || len(req.Tools) == 0 {
		return ""
	}
	provider := ""
	if req.ServingAccount != "" {
		provider = req.ServingAccount
	}
	return WebPreambleForProvider(provider, req.Tools)
}

// WebCatalogOnlyForProvider returns the continuing-thread preamble tailored for the specific provider.
func WebCatalogOnlyForProvider(provider string, defs []types.ToolDef) string {
	if len(defs) == 0 {
		return ""
	}
	if hasBashTool(defs) {
		switch NormalizeWebProvider(provider) {
		case "claude":
			return webToolReminderClaude + "CATALOG\n" + catalogBlock(defs)
		case "gemini":
			return webToolReminderGemini + "CATALOG\n" + catalogBlock(defs)
		default:
			return webToolReminderChatGPT + "CATALOG\n" + catalogBlock(defs)
		}
	}

	example := dynamicToolExample(defs)
	switch NormalizeWebProvider(provider) {
	case "claude":
		return `To inspect files, run terminal commands, or edit code on the repository, invoke tools using <tool_call>:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
The client executes tool calls directly in the workspace and replies with [Tool result].
` + example + "CATALOG\n" + catalogBlock(defs)
	case "gemini":
		return `To inspect files, run terminal commands, or edit code on the repository, invoke tools using <tool_call>:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
The client executes tool calls directly in the workspace and replies with [Tool result].
` + example + "CATALOG\n" + catalogBlock(defs)
	default:
		return `TOOLS LIVE: To inspect files, run terminal commands, or edit code on the repository, invoke tools using <tool_call>:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Built-in python/container tools cannot access this workspace — use <tool_call>, then await [Tool result].
The client executes tool calls directly in the workspace and replies with [Tool result].
` + example + "CATALOG\n" + catalogBlock(defs)
	}
}

// WebCatalogOnly is the continuing-thread preamble: live catalog, no rules essay.
func WebCatalogOnly(defs []types.ToolDef) string {
	return WebCatalogOnlyForProvider("", defs)
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

// catalogLine is "Name" or "Name:key:type,opt?:type,..." — live tools[],
// typed required args, then optional args marked "?" (enums as "a|b").
func catalogLine(d types.ToolDef) string {
	keys := catalogArgs(d.InputSchema, 24)
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

// catalogArgs renders schema args for the web catalog. Optional args carry a
// "?" so web models leave them out: ChatGPT otherwise fills every listed key
// and invents values (Agent isolation "none"/"remote") that the client rejects.
// Enumerated args list their allowed values instead of the bare type.
func catalogArgs(raw json.RawMessage, maxOptional int) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
			Enum []any  `json:"enum"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	required := map[string]bool{}
	for _, k := range s.Required {
		required[k] = true
	}
	format := func(name string) string {
		p := s.Properties[name]
		typ := p.Type
		if len(p.Enum) > 0 {
			vals := make([]string, 0, len(p.Enum))
			for _, v := range p.Enum {
				vals = append(vals, fmt.Sprint(v))
			}
			typ = strings.Join(vals, "|")
		} else if typ == "" {
			typ = "any"
		}
		if required[name] {
			return name + ":" + typ
		}
		return name + "?:" + typ
	}
	out := []string{}
	for _, t := range schemaKeyTypes(raw, maxOptional) {
		name := t
		if i := strings.IndexByte(t, ':'); i > 0 {
			name = t[:i]
		}
		out = append(out, format(name))
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

// WebCloserForProvider returns the closer cue tailored for the provider.
func WebCloserForProvider(provider string) string {
	switch NormalizeWebProvider(provider) {
	case "claude":
		return webToolCloserClaude
	case "gemini":
		return webToolCloserGemini
	default:
		return webToolCloserChatGPT
	}
}

// WebCloser is appended after the flattened transcript so it outranks
// Claude Code's native tool-harness instructions.
func WebCloser() string {
	return WebCloserForProvider("")
}

// MaybeWrapWebStream parses a text-only web stream into ToolCalls when the
// client sent tools[]. Logs extracted tools, then forwards them so Claude
// Code / Cursor can execute locally.
func MaybeWrapWebStream(source string, req *types.ChatRequest, inner <-chan types.StreamChunk) <-chan types.StreamChunk {
	if inner == nil || req == nil || len(req.Tools) == 0 {
		return inner
	}
	proj := req.Project()
	return wrapWebStream(source, req.Tools, req.Messages, inner, proj)
}

type streamThoughtExtractor struct {
	buf             strings.Builder
	lastStreamedIdx int
	inThought       bool
	openTag         string
	closeTag        string
}

func (e *streamThoughtExtractor) Feed(chunk string) (string, string) {
	if chunk == "" {
		return "", ""
	}
	e.buf.WriteString(chunk)
	curr := e.buf.String()
	var emittedThought strings.Builder
	var emittedContent strings.Builder

	for {
		if !e.inThought {
			if e.lastStreamedIdx >= len(curr) {
				break
			}
			rest := curr[e.lastStreamedIdx:]
			lowerRest := strings.ToLower(rest)
			idxThought := strings.Index(lowerRest, "<thought>")
			idxThinking := strings.Index(lowerRest, "<thinking>")
			idxReflection := strings.Index(lowerRest, "<reflection>")

			openIdx := -1
			openTag := ""
			closeTag := ""
			pickEarlier := func(idx int, o, c string) {
				if idx != -1 && (openIdx == -1 || idx < openIdx) {
					openIdx = idx
					openTag = o
					closeTag = c
				}
			}
			pickEarlier(idxThought, "<thought>", "</thought>")
			pickEarlier(idxThinking, "<thinking>", "</thinking>")
			pickEarlier(idxReflection, "<reflection>", "</reflection>")

			if openIdx != -1 {
				if openIdx > 0 {
					emittedContent.WriteString(rest[:openIdx])
				}
				e.inThought = true
				e.openTag = openTag
				e.closeTag = closeTag
				e.lastStreamedIdx += openIdx + len(openTag)
				continue
			}

			safeLen := len(rest)
			for _, prefix := range []string{"<reflection", "<reflect", "<refle", "<refl", "<ref", "<thinking", "<thought", "<think", "<thou", "<tho", "<th", "<t", "<"} {
				if strings.HasSuffix(strings.ToLower(rest), prefix) {
					safeLen -= len(prefix)
					break
				}
			}
			if safeLen > 0 {
				emittedContent.WriteString(rest[:safeLen])
				e.lastStreamedIdx += safeLen
			}
			break
		}

		if e.inThought {
			if e.lastStreamedIdx >= len(curr) {
				break
			}
			rest := curr[e.lastStreamedIdx:]
			lowerRest := strings.ToLower(rest)
			if closeIdx := strings.Index(lowerRest, e.closeTag); closeIdx != -1 {
				delta := rest[:closeIdx]
				e.lastStreamedIdx += closeIdx + len(e.closeTag)
				e.inThought = false
				emittedThought.WriteString(delta)
				continue
			}
			safeLen := len(rest)
			for _, prefix := range []string{"</reflection", "</reflect", "</refle", "</refl", "</ref", "</thinking", "</thought", "</think", "</thou", "</tho", "</th", "</t", "</", "<"} {
				if strings.HasSuffix(strings.ToLower(rest), prefix) {
					safeLen -= len(prefix)
					break
				}
			}
			if safeLen > 0 {
				delta := rest[:safeLen]
				e.lastStreamedIdx += safeLen
				emittedThought.WriteString(delta)
			}
			break
		}
	}
	return emittedThought.String(), emittedContent.String()
}

// maxWebToolCallsPerTurn caps the tool calls taken from one web reply.
// Independent batches (a few Reads, Agent+Skill+MCP) fit; Gemini has replied
// with ~55 calls plus a "review published" summary before any of them ran.
const maxWebToolCallsPerTurn = 5

func wrapWebStream(source string, defs []types.ToolDef, hist []types.ChatMessage, inner <-chan types.StreamChunk, projectRoot ...string) <-chan types.StreamChunk {
	out := make(chan types.StreamChunk, 8)
	go func() {
		defer close(out)
		var buf strings.Builder
		id := source
		hasStreamedThinking := false
		streamedContentLen := 0
		toolMarkupDetected := false
		allowBashFence := !historyHasTools(hist)
		thoughtExt := &streamThoughtExtractor{}
		var pendingContent strings.Builder
		for ch := range inner {
			if ch.ID != "" {
				id = ch.ID
			}
			if ch.Error != nil {
				out <- ch
				return
			}
			th, co := thoughtExt.Feed(ch.Content)
			if ch.Thinking != "" {
				hasStreamedThinking = true
				out <- types.StreamChunk{ID: id, Thinking: ch.Thinking}
			} else if th != "" {
				hasStreamedThinking = true
				out <- types.StreamChunk{ID: id, Thinking: th}
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

			if !toolMarkupDetected {
				currAll := buf.String()
				if hasExplicitWebToolMarkup(currAll) || (allowBashFence && strings.Contains(currAll, "```bash")) || IsToolRefusal(currAll) {
					toolMarkupDetected = true
					pendingContent.Reset()
				} else if containsSuspiciousRefusalPrefix(currAll) {
					// Suspected refusal, defer streaming chunk until complete to prevent refusal leaks
				} else if co != "" {
					pendingContent.WriteString(co)
					if pendingContent.Len() >= 120 {
						toFlush := pendingContent.String()
						out <- types.StreamChunk{ID: id, Content: toFlush}
						streamedContentLen += len(toFlush)
						pendingContent.Reset()
					}
				}
			}
		}
		text := buf.String()
		if !hasStreamedThinking {
			for _, m := range reThought.FindAllStringSubmatch(text, -1) {
				if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
					out <- types.StreamChunk{ID: id, Thinking: strings.TrimSpace(m[1])}
				}
			}
		}
		calls, forced := FinalizeWebToolCalls(text, defs, hist, projectRoot...)
		speculative := false
		if len(calls) > maxWebToolCallsPerTurn {
			// A reply scripting the whole task at once guesses every later
			// argument (commit sha, worktree, review id) and narrates results
			// no tool produced. Run the head only; the narration is fiction.
			monitor.AppendEvent("TOOLS", fmt.Sprintf("%s: %d tool calls in one reply — keeping the first %d", source, len(calls), maxWebToolCallsPerTurn))
			calls = calls[:maxWebToolCallsPerTurn]
			speculative = true
		}
		logWebTools(source, calls, text)
		if len(calls) == 0 {
			cleanText := text
			if hasExplicitWebToolMarkup(text) || strings.Contains(text, "<thought") || strings.Contains(text, "<thinking") {
				cleanText = StripInternalThoughtAndToolTags(text)
			}
			if streamedContentLen < len(cleanText) {
				rem := cleanText[streamedContentLen:]
				if rem != "" {
					out <- types.StreamChunk{ID: id, Content: rem, LogText: text}
				}
			} else if streamedContentLen == 0 && cleanText != "" {
				out <- types.StreamChunk{ID: id, Content: cleanText, LogText: text}
			}
			out <- types.StreamChunk{ID: id, Done: true, LogText: text}
			return
		}
		// API-key style: tool_use only. Drop "please paste" prose.
		if !forced && !speculative {
			if visible := StripWebToolMarkup(text); strings.TrimSpace(visible) != "" {
				if streamedContentLen < len(visible) {
					rem := visible[streamedContentLen:]
					if strings.TrimSpace(rem) != "" {
						out <- types.StreamChunk{ID: id, Content: rem, LogText: text}
					}
				}
			}
		}
		out <- types.StreamChunk{
			ID:               id,
			ToolCalls:        calls,
			FinishReason:     "tool_calls",
			Done:             true,
			LogText:          text,
			ForcedTools:      forced,
			SpeculativeTools: speculative,
		}
	}()
	return out
}

var refusalRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:don't|do not|can't|cannot|unable to|not able to|no|lack)\s+(?:[\w/.-]+\s+){0,4}(?:access to|ability to)\s+(?:[\w/.-]+\s+){0,4}(?:workspace|repository|repo|tools?|bash|git|terminal|mcp)`),
	regexp.MustCompile(`(?i)(?:don't|do not|can't|cannot|unable to|not able to)\s+(?:[\w/.-]+\s+){0,4}(?:inspect|modify|edit|commit|run|execute)\s+(?:[\w/.-]+\s+){0,4}(?:workspace|repository|repo|files?|code|tests?|terminal)`),
	regexp.MustCompile(`(?i)(?:re-enable|provide)\s+(?:[\w/.-]+\s+){0,4}(?:workspace|bash)\s+tool`),
	regexp.MustCompile(`(?i)(?:command\s+execution\s+tool|working\s+bash\s+tool|working\s+tool)\s+(?:is\s+not|isn't)\s+(?:enabled|connected|available)`),
	regexp.MustCompile(`(?i)(?:schema\s+fragment|not\s+a\s+functional\s+tool)`),
	regexp.MustCompile(`(?i)(?:aren't|are not|is not|isn't)\s+(?:[\w/.-]+\s+){0,4}tools?\s+I\s+(?:actually\s+)?have`),
	regexp.MustCompile(`(?i)only\s+the\s+ones\s+listed\s+in\s+my\s+real\s+tool\s+definitions`),
	regexp.MustCompile(`(?i)catalog\s+shown\s+in\s+your\s+message\s+(?:isn't|is\s+not)\s+something\s+I\s+can\s+invoke`),
	regexp.MustCompile(`(?i)(?:don't|do not|can't|cannot)\s+(?:actually\s+)?have\s+access\s+to\s+(?:a\s+)?["']?(?:bash|mcp__[\w_]+)`),
	regexp.MustCompile(`(?i)(?:not\s+something\s+I\s+can\s+invoke\s+directly)`),
	regexp.MustCompile(`(?i)(?:i’m|i\s+am|i'm)\s+ready\s+to\s+help\s+with\s+the\s+workspace.*please\s+provide\s+the\s+specific\s+action`),
	regexp.MustCompile(`(?i)please\s+provide\s+the\s+specific\s+action\s+you\s+want\s+performed`),
	regexp.MustCompile(`(?i)public\s+github\s+api\s+instead`),
	regexp.MustCompile(`(?i)(?:don't|do not|can't|cannot)\s+have\s+(?:a\s+|the\s+)?tools?\s+(?:called|named)?`),
	regexp.MustCompile(`(?i)doesn't\s+add\s+it\s+to\s+my\s+(?:actual\s+)?toolset`),
	regexp.MustCompile(`(?i)the\s+tools\s+I\s+can\s+call\s+are\s+the\s+ones\s+defined\s+for\s+me`),
	regexp.MustCompile(`(?i)(?:function|tool)\s+that\s+(?:doesn't|does not)\s+exist`),
	regexp.MustCompile(`(?i)(?:doesn't|does not)\s+exist\s+for\s+me`),
	regexp.MustCompile(`(?i)stick\s+with\s+my\s+actual\s+tools`),
	regexp.MustCompile(`(?i)calling\s+a\s+(?:function|tool)`),
	regexp.MustCompile(`(?i)(?:can't|cannot|unable to)\s+complete\s+the\s+[\w/.:-]+\s+(?:workflow|task|command|run)`),
	regexp.MustCompile(`(?i)workspace\s+tool\s+runtime\s+(?:[\w/.-]+\s+){0,6}(?:is\s+not|isn't)\s+(?:available|enabled|connected)`),
	regexp.MustCompile(`(?i)(?:tool\s+runtime|command\s+wrapper|posting\s+tools|checkout/context\s+tools)\s+(?:[\w/.-]+\s+){0,6}(?:is\s+not|isn't)\s+(?:available|enabled|connected)`),
	regexp.MustCompile(`(?i)if\s+you\s+run\s+this\s+in\s+the\s+claude\s+code\s+workspace\s+session`),
	regexp.MustCompile(`(?i)in\s+this\s+chat\s+instance\s+because\s+the\s+[\w/.-]+\s+tool`),
	regexp.MustCompile(`(?i)open-pr\s+runtime\s+is\s+not\s+available`),
	// ChatGPT's canned reply when its own python tool is unavailable.
	regexp.MustCompile(`(?i)can'?t\s+do\s+more\s+advanced\s+data\s+analysis`),
	regexp.MustCompile(`(?i)(?:không\s+thể|chưa\s+thể|không\s+có\s+quyền)\s+(?:[\w/.-]+\s+){0,4}(?:truy\s+cập|thao\s+tác|chạy|thực\s+thi)\s+(?:[\w/.-]+\s+){0,4}(?:repo|repository|workspace|hệ\s+thống|lệnh|công\s+cụ)`),
}

var (
	reStallIntent    = regexp.MustCompile(`(?i)(?:^|[.!:\n]\s*)(?:i\s+need\s+to|i(?:'ll|\s+will)|let\s+me|let's|let\s+us|now\s+i(?:'ll|\s+will)|next,?\s+i(?:'ll|\s+will)|i'm\s+going\s+to|i\s+am\s+going\s+to)\s+(?:now\s+)?(?:continue|proceed|run|execute|call|use|launch|invoke|check|read|inspect|fix|edit|write|create|start|try|retry)\b`)
	reStallRemaining = regexp.MustCompile(`(?i)\bremaining\s+(?:required\s+)?(?:tool\s+)?steps\b`)
	reStallOffer     = regexp.MustCompile(`(?i)\b(?:if\s+you(?:'d)?\s+(?:want|like)|would\s+you\s+like|let\s+me\s+know|shall\s+i|do\s+you\s+want)\b`)
)

var reLostTask = regexp.MustCompile(`(?i)\b(?:how\s+can\s+i\s+(?:help|assist)(?:\s+you)?|what\s+would\s+you\s+like\s+(?:me\s+)?to\s+do|(?:i\s+am|i'm)\s+ready\s+to\s+(?:help|assist)|please\s+(?:provide|share|tell\s+me)\s+(?:the\s+|your\s+)?(?:task|request|instructions))\b`)

// IsLostTask detects a reply that forgot the task it was working on — a
// greeting or "how can I help you today?" — which Gemini Web gives after a
// tool result full of prompt-like text (e.g. reviewing a diff of prompts).
// Only meaningful mid tool loop: as a first reply to "hi" it is an answer.
func IsLostTask(text string) bool {
	if hasExplicitWebToolMarkup(text) {
		return false
	}
	t := strings.TrimSpace(StripInternalThoughtAndToolTags(text))
	t = strings.ReplaceAll(t, "’", "'")
	return t != "" && len([]rune(t)) <= 800 && reLostTask.MatchString(t)
}

// IsToolStall detects a short web reply that announces the next action
// ("I need to continue by running the remaining required tool steps.") but
// carries no <tool_call>, so nothing runs and the client's turn ends.
// Offers to the user ("If you want, I'll…") are answers, not stalls.
func IsToolStall(text string) bool {
	if hasExplicitWebToolMarkup(text) {
		return false
	}
	t := strings.TrimSpace(StripInternalThoughtAndToolTags(text))
	t = strings.ReplaceAll(t, "’", "'")
	if t == "" || len([]rune(t)) > 600 || reStallOffer.MatchString(t) {
		return false
	}
	return reStallIntent.MatchString(t) || reStallRemaining.MatchString(t)
}

// IsToolRefusal detects when a web model hallucinates that it lacks tool access
// despite tools being live and available in the catalog.
func IsToolRefusal(text string) bool {
	lower := strings.ToLower(text)
	lower = strings.ReplaceAll(lower, "’", "'")
	lower = strings.ReplaceAll(lower, "‘", "'")
	lower = strings.ReplaceAll(lower, "“", "\"")
	lower = strings.ReplaceAll(lower, "”", "\"")
	for _, re := range refusalRegexes {
		if re.MatchString(lower) {
			return true
		}
	}
	refusalKeywords := []string{
		"không có quyền truy cập công cụ",
		"không có quyền truy cập repo",
		"không có quyền truy cập vào repo",
		"không có quyền truy cập workspace",
		"không có công cụ repo",
		"không có công cụ",
		"chưa có công cụ",
		"thiếu công cụ",
		"chưa kết nối công cụ",
		"chưa nhận được tool runtime",
		"không có tool runtime",
		"không có tool",
		"chỉ có catalog dạng văn bản",
		"chỉ có catalog",
		"không thể gọi <tool_call>",
		"khi tool repo được bật lại",
		"bật lại phiên có tool",
		"gửi lại ngữ cảnh tool",
		"phiên này chưa cung cấp tool",
		"cần quyền truy cập repo qua tool",
		"chưa thể thực hiện commit trực tiếp",
		"không thể thực hiện commit trực tiếp",
		"chưa thể commit trực tiếp",
		"không thể commit trực tiếp",
		"không thể tự commit",
		"không thể tự `git",
		"không thể tự git",
		"mình chưa thể thực hiện commit",
		"chưa thể bắt đầu thao tác vì",
		"không có quyền thao tác",
		"không thể thao tác trực tiếp trên repo",
		"không thể thao tác trực tiếp",
		"chưa thể thao tác trực tiếp",
		"không có quyền gọi",
		"không có quyền gọi các tool",
		"không thể trung thực nói rằng đã sửa file",
		"mở lại phiên có workspace tools",
		"mở lại phiên có",
		"thao tác trực tiếp trên repo thật",
		"để thao tác trực tiếp",
		"chưa thực sự ghi được vào repo",
		"phiên hiện tại mình không có",
		"phiên hiện tại không có",
		"workspace tools",
		"don't have access to tools",
		"do not have access to tools",
		"don't have access to the repository",
		"don't have access to the repo",
		"don't have access to the workspace",
		"don't have access to the project",
		"do not have access to the repository",
		"do not have access to the repo",
		"do not have access to the workspace",
		"repository workspace tool",
		"workspace tool in this chat",
		"workspace tool session",
		"access the repository workspace",
		"access the workspace from this chat",
		"provided workspace tool",
		"workspace tool is not actually available",
		"workspace tool is not available",
		"workspace tool isn't available",
		"tool is not actually available",
		"tools are not actually available",
		"tool is not available to me",
		"tools are not available to me",
		"not actually available to me",
		"not available to me in this chat",
		"can't execute the '/",
		"cannot execute the '/",
		"can't execute the \"/",
		"cannot execute the \"/",
		"can't execute the `/",
		"cannot execute the `/",
		"can't execute the command",
		"cannot execute the command",
		"unable to execute the command",
		"can't execute commands directly",
		"cannot execute commands directly",
		"unable to execute commands directly",
		"can't directly execute commands",
		"cannot directly execute commands",
		"cannot execute git",
		"can't execute git",
		"unable to execute git",
		"no access to the repository workspace",
		"no direct access to the repository workspace",
		"cannot directly interact with the repository",
		"can't directly interact with the repository",
		"cannot directly interact with your repository",
		"can't directly interact with your repository",
		"i cannot interact with the repository",
		"i can't interact with the repository",
		"don't have the ability to execute",
		"do not have the ability to execute",
		"unable to run commands in this session",
		"cannot run commands in this session",
		"can't run commands in this session",
		"i lack the ability to run",
		"i lack the tools to",
		"do not have workspace tools",
		"don't have workspace tools",
		"does not have workspace tools",
		"doesn't have workspace tools",
		"not able to run",
		"i'm not able to run",
		"i am not able to run",
		"not able to run the '/",
		"not able to run the",
		"not able to execute",
		"i'm not able to execute",
		"i am not able to execute",
		"isn't connected to my available tools",
		"not connected to my available tools",
		"isn't connected to my tools",
		"not connected to my tools",
		"not connected to the workspace",
		"isn't connected to the workspace",
		"isn't connected to",
		"not connected to",
		"tool catalog shown in your message",
		"in this conversation environment",
		"in this chat environment",
		"available tools here",
		"workspace-enabled session",
		"where the bash tool call actually executes",
		"where the `bash` tool call actually executes",
		"where the bash tool actually executes",
		"where the `bash` tool actually executes",
		"once i can access the code",
		"once i have access to the code",
		"once i have access to the repository",
		"to proceed with pr",
		"working bash tool",
		"working tool",
		"schema fragment",
		"not a functional tool",
		"functional tool i can invoke",
		"functional tool",
		"no real git access",
		"no real network",
		"no real filesystem",
		"no real network, filesystem",
		"assume a capability i don't have",
		"genuine tool-use setup",
		"where tools are actually wired up",
		"tools are actually wired up",
		"tools are actually wired",
		"tools are wired up",
		"i don't actually have a working",
		"i do not actually have a working",
		"i don't have a working",
		"i do not have a working",
		"claiming tool results are",
		"suggesting otherwise",
		"can't fix a pr blind",
		"cannot fix a pr blind",
		"i can't verify or act on pr",
		"i cannot verify or act on pr",
		"can't verify or act on pr",
		"cannot verify or act on pr",
		"a few things to flag honestly",
		"things to flag honestly",
		"i should be upfront about that",
		"this session does not have access",
		"this session doesn't have access",
		"không thể thao tác với repository",
		"không thể truy cập repository",
		"không thể truy cập workspace",
		"không có quyền truy cập vào workspace",
		"can't inspect or modify",
		"cannot inspect or modify",
		"no access to the repository",
		"no access to the repo",
		"cannot access the repo",
		"cannot access the workspace",
		"cannot execute commands on the repository",
		"i lack tool access",
		"i do not have tool access",
		"only text catalog",
		"only a text catalog",
		"i cannot run bash",
		"i cannot edit files directly",
		"cannot commit directly",
		"unable to run git",
		"don't have permission to call tools",
		"cannot call tools in this session",
		"do not have direct access to modify",
		"cannot directly modify",
		"cannot directly write",
		"tôi không thể chạy lệnh",
		"tôi không thể thực hiện lệnh",
		"mình không thể chạy lệnh",
		"mình không thể thực hiện lệnh",
		"không thể thực thi lệnh",
		"không có khả năng thực thi lệnh",
		"không thể trực tiếp chỉnh sửa",
		"không thể chỉnh sửa trực tiếp",
		"không có quyền truy cập vào hệ thống",
		"không có quyền truy cập hệ thống",
		"không có quyền truy cập filesystem",
		"không có quyền truy cập file",
		"tôi là một mô hình ngôn ngữ",
		"là một mô hình ai",
		"as an ai language model, i cannot",
		"as an ai, i cannot",
		"as an ai, i do not have access",
		"i am unable to execute commands",
		"i cannot execute commands",
		"cannot execute terminal commands",
		"unable to execute terminal commands",
		"i cannot run commands",
		"unable to run commands",
		"i don't have access to your local machine",
		"i cannot access your local files",
		"no access to local files",
		"cannot run terminal commands",
		"please run the following command",
		"hãy chạy lệnh sau trên terminal của bạn",
		"bạn hãy tự chạy lệnh",
		"vui lòng chạy lệnh",
		"i cannot make changes directly",
		"cannot make changes directly",
		"text-based simulation",
		"text simulation",
		"not actually claude code",
		"not a live claude code session",
		"fake tool call",
		"fake tool calls",
		"fake \"tool_call\"",
		"fake tool_call",
		"fake protocol",
		"adopt a fake",
		"custom xml format",
		"can't access the project workspace",
		"can't access the workspace",
		"cannot access the project workspace",
		"aren't part of my real toolset",
		"not part of my real toolset",
		"not connected to this environment",
		"arbitrary terminal commands directly",
		"workspace tool isn't available",
		"workspace tool is not available",
		"in a chat interface right now",
		"in a chat interface",
		"external harness",
		"harness prompt",
		"prompt injection",
		"relaying these blocks to a real shell",
		"asking for a lot of trust",
		"i'm not going to follow that framing",
		"not going to follow that framing",
		"pretend to have made edits",
		"pretend to have run commands",
		"can't actually act on your repo from here",
		"cannot actually act on your repo from here",
		"i don't have a verified way to confirm",
		"i do not have a verified way to confirm",
		"supposedly execute against a real repo",
		"is not something i actually have access to",
		"not something i actually have access to",
		"fictional interface",
		"fake instructions",
		"asserted as \"live\"",
		"asserted as 'live'",
		"asserted as live",
		"i'll be direct",
		"i will be direct",
		"i want to be straightforward",
		"no matter how many times it's asserted",
		"isn't a real tool i have access to",
		"not a real tool i have access to",
		"won't pretend to invoke it",
		"can't make it \"go live\"",
		"can't make it go live",
		"no clone of that repository",
		"no write access",
		"no github auth token",
		"what i can't do:",
		"what i cannot do:",
		"is not a real operation available to me",
		"isn't a real operation available to me",
		"format is not something i",
		"pretending to use a tool",
		"tool in my actual toolset",
		"in my actual toolset",
		"fake command output",
		"fabricated, fake",
		"not going to emit",
		"i'm not going to emit",
		"i am not going to emit",
		"same instructions keep coming back",
		"keep repeating this plainly",
		"there is no bash",
		"no bash/tool_name",
		"ignoring that framing",
		"still ignoring that framing",
		"not the fictional",
		"fictional format",
		"from your message template",
		"message template",
		"fictional bash",
		"fictional <tool_call>",
		"fictional \u003ctool_call\u003e",
		"just to re-ground where things stand",
		"i'm still ignoring",
		"i am still ignoring",
	}
	for _, kw := range refusalKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func containsSuspiciousRefusalPrefix(text string) bool {
	lower := strings.ToLower(text)
	prefixes := []string{
		"không có quyền",
		"không thể",
		"chưa thể",
		"chưa có",
		"thiếu công cụ",
		"cannot",
		"unable to",
		"don't have access",
		"do not have access",
		"i lack",
		"as an ai",
		"i need to flag",
		"this session is not",
		"i can't execute",
		"i cannot execute",
		"i am unable to execute",
		"i'm unable to execute",
		"not actually available",
		"provided workspace tool",
		"workspace tool is",
		"workspace tool isn't",
		"i'm not able",
		"i am not able",
		"not able to",
		"not able to run",
		"isn't connected",
		"not connected",
		"tool catalog shown",
		"in this conversation environment",
		"in this chat environment",
		"to proceed with pr",
		"to proceed with",
		"i don't actually have",
		"i do not actually have",
		"working bash tool",
		"a few things to flag",
		"i should be upfront",
		"aren't tools i actually have",
		"are not tools i actually have",
		"only the ones listed in my real tool definitions",
		"catalog shown in your message",
		"please provide the specific action",
		"i'll try fetching this via the public github api",
		"i'll stick with my actual tools",
		"stick with my actual tools",
		"calling a function that doesn't exist",
		"i can't complete the /open-pr",
		"i cannot complete the /open-pr",
		"workspace tool runtime described in the prompt",
		"if you run this in the claude code workspace session",
	}
	for _, p := range prefixes {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

var (
	reBacktickCmd   = regexp.MustCompile("`((?:git|ls|find|cat|head|tail|grep|cargo|go|npm|pnpm|yarn|make|python|pytest|sh|bash)\\s+[^`]+)`")
	reCommandSplit  = regexp.MustCompile(`&&|\|\||;|\|`)
	readOnlyGitSubs = map[string]bool{"status": true, "diff": true, "log": true, "show": true, "branch": true, "remote": true, "rev-parse": true, "ls-files": true, "grep": true, "blame": true}
	readOnlyCmds    = map[string]bool{"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "rg": true, "find": true, "wc": true, "pwd": true, "echo": true}
)

// isReadOnlyCommand reports whether every segment of a shell command only
// reads: git status/diff/log/…, ls, cat, grep, find without -delete/-exec, no
// redirection. Commands lifted from prose run without the model asking, so
// nothing that writes (git add/commit/push, rm, >) may pass.
func isReadOnlyCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "><`$") || strings.Contains(cmd, "\n") {
		return false
	}
	for _, seg := range reCommandSplit.Split(cmd, -1) {
		f := strings.Fields(seg)
		if len(f) == 0 {
			return false
		}
		switch {
		case f[0] == "git":
			if len(f) < 2 || !readOnlyGitSubs[f[1]] {
				return false
			}
			for _, a := range f[2:] {
				if a == "-d" || a == "-D" || a == "-m" || a == "-M" || a == "--delete" || a == "add" || a == "remove" || a == "set-url" {
					return false
				}
			}
		case readOnlyCmds[f[0]]:
			for _, a := range f[1:] {
				if a == "-delete" || a == "-exec" || a == "-execdir" || a == "-ok" {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// isAgenticSkillTask reports whether the user prompt contains an explicit skill command or repo action task.
func isAgenticSkillTask(task string) bool {
	p := strings.ToLower(CleanUserTurnContent(task))
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return true
	}
	if strings.Contains(p, "github.com/") && strings.Contains(p, "/pull/") {
		return true
	}
	if strings.Contains(p, "open-pr") || strings.Contains(p, "review pr") || strings.Contains(p, "fix pr") {
		return true
	}
	if strings.Contains(p, "mcp") || strings.Contains(p, "plugin") || strings.Contains(p, "skill") {
		return true
	}
	return false
}

// FinalizeWebToolCalls parses web text into tool calls and coerces arg keys to the client dialect schema.
func FinalizeWebToolCalls(text string, defs []types.ToolDef, hist []types.ChatMessage, projectRoot ...string) (calls []types.ToolCall, forced bool) {
	if strings.TrimSpace(text) == "" || len(defs) == 0 {
		return nil, false
	}
	// When prior tool history exists or explicit markup is present, avoid interpreting plain markdown bash codeblocks as tool executions.
	allowBashFence := !historyHasTools(hist) && !hasExplicitWebToolMarkup(text)
	calls = parseWebTools(text, defs, allowBashFence)
	if len(calls) == 0 {
		userGoal := lastUserText(hist)
		isSkill := isAgenticSkillTask(userGoal)
		isRefusal := IsToolRefusal(text)
		if isSkill || isRefusal {
			by := make(map[string]types.ToolDef, len(defs))
			for _, d := range defs {
				by[strings.ToLower(d.Name)] = d
			}
			bashDef, hasBash := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command", "shell")
			isExplicitMCPRequest := (strings.Contains(strings.ToLower(userGoal), "mcp") && !goalNamesNonMCPTool(userGoal, defs)) ||
				(len(defs) == 1 && strings.HasPrefix(strings.ToLower(defs[0].Name), "mcp__"))

			if hasBash && !isExplicitMCPRequest {
				// Commands are lifted from prose only when the reply refused or
				// stalled ("I will start by running `git status`"), and only
				// read-only ones: prose that merely mentions "`git add -A` is
				// forbidden" was run as `git add`, again and again.
				liftFromProse := isRefusal || IsToolStall(text)
				// Tier 1: Check markdown codeblocks (reBashFence)
				if liftFromProse && reBashFence.MatchString(text) {
					for _, m := range reBashFence.FindAllStringSubmatch(text, -1) {
						cmd := strings.TrimSpace(m[1])
						if cmd == "" || !isReadOnlyCommand(cmd) {
							continue
						}
						b, _ := json.Marshal(map[string]string{"command": cmd})
						calls = append(calls, types.ToolCall{
							ID:        newWebToolID(),
							Name:      bashDef.Name,
							Arguments: string(b),
						})
						forced = true
					}
				}
				// Tier 2: Check inline backtick commands (reBacktickCmd)
				if len(calls) == 0 && liftFromProse {
					if match := reBacktickCmd.FindStringSubmatch(text); len(match) > 1 && isReadOnlyCommand(match[1]) {
						cmd := strings.TrimSpace(match[1])
						b, _ := json.Marshal(map[string]string{"command": cmd})
						calls = append(calls, types.ToolCall{
							ID:        newWebToolID(),
							Name:      bashDef.Name,
							Arguments: string(b),
						})
						forced = true
					}
				}
				// Tier 3: Auto-kickstart tool loop on refusal
				if len(calls) == 0 && isRefusal {
					lowerUser := strings.ToLower(userGoal)
					cmd := "git status"
					if strings.Contains(lowerUser, "open-pr") || strings.Contains(lowerUser, "pr") {
						cmd = "git status && git branch --show-current && git remote -v"
					} else if strings.Contains(lowerUser, "diff") || strings.Contains(lowerUser, "review") {
						cmd = "git diff"
					} else if strings.Contains(lowerUser, "log") {
						cmd = "git log -n 5 --oneline"
					}
					b, _ := json.Marshal(map[string]string{"command": cmd})
					calls = append(calls, types.ToolCall{
						ID:        newWebToolID(),
						Name:      bashDef.Name,
						Arguments: string(b),
					})
					forced = true
				}
			}

			// Non-Bash tool fallback / MCP kickstart on refusal
			if len(calls) == 0 && isRefusal {
				if tc, ok := autoKickstartMatchingTool(userGoal, defs); ok {
					calls = append(calls, tc)
					forced = true
				}
			}
		}
	}
	calls = uniqueWebToolIDs(calls, hist)
	return coerceAllToolArgs(calls, defs, projectRoot...), forced
}

func autoKickstartMatchingTool(userGoal string, defs []types.ToolDef) (types.ToolCall, bool) {
	if len(defs) == 0 {
		return types.ToolCall{}, false
	}
	target := defs[0]
	if len(defs) > 1 {
		lowerGoal := strings.ToLower(userGoal)
		bestScore := -1 << 30
		for _, d := range defs {
			score := 0
			// A tool the user named verbatim is the one they asked for.
			if len(d.Name) > 3 && strings.Contains(lowerGoal, strings.ToLower(d.Name)) {
				score += 100
			}
			parts := strings.FieldsFunc(strings.ToLower(d.Name), func(r rune) bool {
				return r == '_' || r == '-' || r == '.'
			})
			for _, p := range parts {
				if len(p) > 2 && p != "mcp" && strings.Contains(lowerGoal, p) {
					score += 2
				}
			}
			// Required string args synthesized empty fail validation on the
			// client ("file_content and issue are required") — prefer tools
			// that can actually run.
			score -= 10 * emptyRequiredArgs(synthesizeToolArguments(userGoal, d.InputSchema), d.InputSchema)
			if score > bestScore {
				bestScore = score
				target = d
			}
		}
	}

	args := synthesizeToolArguments(userGoal, target.InputSchema)
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte("{}")
	}
	return types.ToolCall{
		ID:        newWebToolID(),
		Name:      target.Name,
		Arguments: string(b),
	}, true
}

// goalNamesNonMCPTool reports whether the user goal names a non-MCP tool
// (e.g. "Bash", "Edit") as a word, so a goal that merely also mentions MCP
// still gets the Bash fallbacks.
func goalNamesNonMCPTool(userGoal string, defs []types.ToolDef) bool {
	patterns := make([]*regexp.Regexp, 0, len(defs))
	for _, d := range defs {
		if strings.HasPrefix(strings.ToLower(d.Name), "mcp__") || len(d.Name) < 3 {
			continue
		}
		patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(d.Name)+`\b`))
	}
	for _, re := range patterns {
		if re.MatchString(userGoal) {
			return true
		}
	}
	return false
}

// emptyRequiredArgs counts required arguments that synthesis left empty.
func emptyRequiredArgs(args map[string]any, schemaRaw json.RawMessage) int {
	var schema struct {
		Required []string `json:"required"`
	}
	if len(schemaRaw) == 0 || json.Unmarshal(schemaRaw, &schema) != nil {
		return 0
	}
	n := 0
	for _, k := range schema.Required {
		switch v := args[k].(type) {
		case nil:
			n++
		case string:
			if strings.TrimSpace(v) == "" {
				n++
			}
		}
	}
	return n
}

func synthesizeToolArguments(userGoal string, schemaRaw json.RawMessage) map[string]any {
	args := make(map[string]any)
	if len(schemaRaw) == 0 {
		return args
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
		} `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &schema) != nil {
		return args
	}

	lowerGoal := strings.ToLower(userGoal)
	reOwnerRepo := regexp.MustCompile(`([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)`)
	ownerRepoMatch := reOwnerRepo.FindStringSubmatch(userGoal)

	for propName := range schema.Properties {
		lowerProp := strings.ToLower(propName)
		switch {
		case lowerProp == "owner":
			if len(ownerRepoMatch) > 1 {
				args[propName] = ownerRepoMatch[1]
			}
		case lowerProp == "repo" || lowerProp == "repository":
			if len(ownerRepoMatch) > 2 {
				args[propName] = ownerRepoMatch[2]
			}
		case lowerProp == "state":
			if strings.Contains(lowerGoal, "closed") {
				args[propName] = "closed"
			} else if strings.Contains(lowerGoal, "all") {
				args[propName] = "all"
			} else {
				args[propName] = "open"
			}
		case lowerProp == "command" || lowerProp == "cmd":
			args[propName] = userGoal
		case lowerProp == "query" || lowerProp == "search" || lowerProp == "q":
			args[propName] = userGoal
		case lowerProp == "path" || lowerProp == "filepath" || lowerProp == "file_path" || lowerProp == "absolutepath":
			rePath := regexp.MustCompile(`(?:/[\w.-]+)+`)
			if m := rePath.FindString(userGoal); m != "" {
				args[propName] = m
			}
		}
	}

	for _, reqKey := range schema.Required {
		if _, ok := args[reqKey]; !ok {
			if prop, exists := schema.Properties[reqKey]; exists {
				switch prop.Type {
				case "string":
					args[reqKey] = ""
				case "integer", "number":
					args[reqKey] = 0
				case "boolean":
					args[reqKey] = false
				case "array":
					args[reqKey] = []any{}
				case "object":
					args[reqKey] = map[string]any{}
				default:
					args[reqKey] = ""
				}
			}
		}
	}
	return args
}

func lastUserText(hist []types.ChatMessage) string {
	for i := len(hist) - 1; i >= 0; i-- {
		if strings.EqualFold(hist[i].Role, "user") {
			cleaned := CleanUserTurnContent(hist[i].Content)
			if cleaned != "" {
				return cleaned
			}
		}
	}
	return ""
}

// newWebToolID mints a tool_use id for a call parsed from web text.
func newWebToolID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "toolu_web_" + hex.EncodeToString(b[:])
}

// uniqueWebToolIDs gives each call an id unused in hist and in the batch.
// Claude Code drops a tool_use whose id already appears in the transcript and
// sends "(no content)" as its result, so the model never sees the output and
// ends up claiming it has no tools.
func uniqueWebToolIDs(calls []types.ToolCall, hist []types.ChatMessage) []types.ToolCall {
	used := map[string]bool{}
	for _, m := range hist {
		if m.ToolCallID != "" {
			used[m.ToolCallID] = true
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				used[tc.ID] = true
			}
		}
	}
	for i := range calls {
		if calls[i].ID == "" || used[calls[i].ID] {
			calls[i].ID = newWebToolID()
		}
		used[calls[i].ID] = true
	}
	return calls
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
		strings.Contains(lower, "<tool-call") ||
		strings.Contains(lower, "<function_call") ||
		strings.Contains(lower, "[tool_call") ||
		strings.Contains(lower, "[tool call") ||
		strings.Contains(lower, "<invoke") ||
		strings.Contains(lower, "<<<amux_tool") ||
		strings.Contains(lower, "call:default_api:") ||
		strings.Contains(lower, "call:") ||
		strings.Contains(lower, "action:") ||
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
	keys := schemaKeys(d.InputSchema, 30)
	if len(keys) == 0 {
		if len(prefer) > 0 {
			return prefer[0]
		}
		return ""
	}
	for _, p := range prefer {
		for _, k := range keys {
			if strings.EqualFold(k, p) {
				return k
			}
		}
	}
	return ""
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
func ParseWebTools(text string, defs []types.ToolDef, projectRoot ...string) []types.ToolCall {
	return coerceAllToolArgs(parseWebTools(text, defs, true), defs, projectRoot...)
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
			lower == "run_command" || lower == "exec_command" || lower == "terminal" ||
			lower == "execute_bash" || lower == "run_shell_command" || lower == "cmd" ||
			lower == "sh" || lower == "bash_command" || lower == "execute_command":
			if d, ok := findToolDef(by, "bash", "run_terminal_command", "run_command", "exec_command", "shell"); ok {
				return d.Name, true
			}
		case lower == "read" || lower == "read_file" || lower == "view_file" ||
			lower == "view" || lower == "cat" || lower == "show_file" ||
			lower == "open_file" || lower == "get_file_content" || lower == "readfile":
			if d, ok := findToolDef(by, "read", "read_file", "view_file", "view"); ok {
				return d.Name, true
			}
		case lower == "write" || lower == "write_file" || lower == "write_to_file" ||
			lower == "create_file" || lower == "new_file" || lower == "save_file" ||
			lower == "overwrite_file" || lower == "touch" || lower == "writefile":
			if d, ok := findToolDef(by, "write", "write_file", "write_to_file"); ok {
				return d.Name, true
			}
		case lower == "edit" || lower == "edit_file" || lower == "replace_file_content" ||
			lower == "patch" || lower == "str_replace_editor" || lower == "replace" ||
			lower == "modify_file" || lower == "update_file" || lower == "apply_patch" ||
			lower == "patch_file" || lower == "editfile":
			if d, ok := findToolDef(by, "edit", "edit_file", "replace_file_content", "patch", "str_replace_editor", "replace"); ok {
				return d.Name, true
			}
		case lower == "grep" || lower == "grep_search" || lower == "search_code" ||
			lower == "search" || lower == "ripgrep" || lower == "find_in_files" ||
			lower == "code_search" || lower == "search_files":
			if d, ok := findToolDef(by, "grep", "grep_search", "search_code", "search"); ok {
				return d.Name, true
			}
		case lower == "find" || lower == "find_by_name" || lower == "glob" ||
			lower == "file_search" || lower == "locate_files" || lower == "find_files":
			if d, ok := findToolDef(by, "find", "find_by_name", "glob", "file_search"); ok {
				return d.Name, true
			}
		case lower == "list_dir" || lower == "list_directory" || lower == "ls" || lower == "dir" || lower == "list_files":
			if d, ok := findToolDef(by, "list_dir", "list_directory", "list_files", "glob", "find_by_name"); ok {
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
		case lower == "workflow" || lower == "run_workflow" || lower == "execute_workflow" || lower == "apply_workflow":
			if d, ok := findToolDef(by, "workflow", "run_workflow", "execute_workflow", "apply_workflow"); ok {
				return d.Name, true
			}
		}

		// MCP tool matching: e.g. "mcp__server__tool" <-> "server_tool" or "mcp_server_tool"
		cleanName := strings.TrimPrefix(lower, "mcp__")
		cleanName = strings.TrimPrefix(cleanName, "mcp_")
		cleanName = strings.TrimPrefix(cleanName, "mcp.")
		cleanNameNorm := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(cleanName, "__", "_"), "-", "_"), ".", "_")
		cleanNameNorm = strings.ReplaceAll(cleanNameNorm, "/", "_")
		for k, canon := range allow {
			kClean := strings.TrimPrefix(k, "mcp__")
			kClean = strings.TrimPrefix(kClean, "mcp_")
			kClean = strings.TrimPrefix(kClean, "mcp.")
			kCleanNorm := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(kClean, "__", "_"), "-", "_"), ".", "_")
			kCleanNorm = strings.ReplaceAll(kCleanNorm, "/", "_")
			if kCleanNorm == cleanNameNorm || strings.HasSuffix(kCleanNorm, "_"+cleanNameNorm) || strings.HasSuffix(cleanNameNorm, "_"+kCleanNorm) {
				return canon, true
			}
		}

		// AGY call_mcp_tool fallback: if client has call_mcp_tool and incoming is an MCP tool
		if strings.HasPrefix(lower, "mcp__") || strings.HasPrefix(lower, "mcp_") || strings.Contains(lower, "__") {
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
				var mcpRaw map[string]json.RawMessage
				if json.Unmarshal([]byte(args), &mcpRaw) == nil {
					var serverName, toolName string
					for _, sk := range []string{"ServerName", "server_name", "server", "Server", "serverName"} {
						if v, found := mcpRaw[sk]; found {
							_ = json.Unmarshal(v, &serverName)
							if serverName != "" {
								break
							}
						}
					}
					for _, tk := range []string{"ToolName", "tool_name", "tool", "Tool", "toolName"} {
						if v, found := mcpRaw[tk]; found {
							_ = json.Unmarshal(v, &toolName)
							if toolName != "" {
								break
							}
						}
					}
					if serverName != "" && toolName != "" {
						candidate := "mcp__" + serverName + "__" + toolName
						if c, found := canonical(candidate); found {
							canon = c
							ok = true
							for _, ak := range []string{"Arguments", "arguments", "args", "params"} {
								if v, f := mcpRaw[ak]; f && len(v) > 0 && string(v) != "null" {
									args = string(v)
									break
								}
							}
						}
					}
				}
			}
			if !ok {
				return
			}
		}

		// If client expects call_mcp_tool and incoming is an MCP tool name (e.g. mcp__server__tool or server__tool):
		if strings.EqualFold(canon, "call_mcp_tool") && !strings.EqualFold(name, "call_mcp_tool") {
			server, tool := SplitMCPServerTool(name)
			if server != "" && tool != "" {
				var innerArgs any
				if json.Unmarshal([]byte(args), &innerArgs) == nil {
					wrapped := map[string]any{
						"ServerName":  server,
						"ToolName":    tool,
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
		if !json.Valid([]byte(args)) && len(by) == 0 {
			// No catalog to check against; with one, objectToolArgs maps
			// the bare value onto the tool's own required key.
			b, _ := json.Marshal(map[string]string{"command": args})
			args = string(b)
		}
		if id == "" {
			id = newWebToolID()
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
	for _, m := range reHyphenTool.FindAllStringSubmatch(text, -1) {
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
	for _, m := range reActionTool.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[1])
		rawArgs := strings.TrimSpace(m[2])
		if n, i, args, ok := parseToolCallJSON(rawArgs); ok {
			if n != "" {
				name = n
			}
			add(name, i, args)
			continue
		}
		add(name, "", rawArgs)
	}
	for _, m := range reAMUXTool.FindAllStringSubmatch(text, -1) {
		add(m[1], m[2], m[3])
	}
	for _, m := range reGeminiCall.FindAllStringSubmatch(text, -1) {
		name, raw := m[1], strings.TrimSpace(m[2])
		if n, i, a, ok := parseToolCallJSON(raw); ok {
			if n == "" {
				n = name
			}
			add(n, i, a)
		} else if quoted := reBareJSONKey.ReplaceAllString(raw, `$1"$2":`); json.Valid([]byte(quoted)) {
			// Gemini's native syntax: call:default_api:view_file{AbsolutePath: "/a.go"}
			add(name, "", quoted)
		} else if parsed, ok := pyKwargsToJSON(raw); ok {
			add(name, "", parsed)
		} else {
			add(name, "", raw)
		}
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
				// A fence is an example as often as an action: only
				// read-only commands run without an explicit <tool_call>.
				if cmd == "" || !isReadOnlyCommand(cmd) {
					continue
				}
				b, _ := json.Marshal(map[string]string{"command": cmd})
				add(bashName, "", string(b))
			}
		}
	}
	return out
}

// pyKwargsToJSON converts Python-style keyword arguments inside parentheses,
// e.g. (AbsolutePath="/path/to/file", StartLine=1, is_dir=False), into valid JSON object string.
func pyKwargsToJSON(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return "", false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return "{}", true
	}

	result := make(map[string]any)
	n := len(inner)
	i := 0

	for i < n {
		for i < n && (inner[i] == ' ' || inner[i] == '\t' || inner[i] == '\r' || inner[i] == '\n' || inner[i] == ',') {
			i++
		}
		if i >= n {
			break
		}

		keyStart := i
		for i < n && inner[i] != '=' && inner[i] != ' ' && inner[i] != '\t' && inner[i] != ',' && inner[i] != ':' {
			i++
		}
		key := strings.TrimSpace(inner[keyStart:i])
		if key == "" {
			break
		}

		for i < n && (inner[i] == ' ' || inner[i] == '\t') {
			i++
		}
		if i < n && (inner[i] == '=' || inner[i] == ':') {
			i++
		}
		for i < n && (inner[i] == ' ' || inner[i] == '\t') {
			i++
		}
		if i >= n {
			result[key] = ""
			break
		}

		if inner[i] == '"' || inner[i] == '\'' {
			quote := inner[i]
			i++
			var valSb strings.Builder
			for i < n {
				if inner[i] == '\\' && i+1 < n {
					next := inner[i+1]
					if next == quote || next == '\\' {
						valSb.WriteByte(next)
						i += 2
						continue
					} else if next == 'n' {
						valSb.WriteByte('\n')
						i += 2
						continue
					} else if next == 't' {
						valSb.WriteByte('\t')
						i += 2
						continue
					}
					valSb.WriteByte(next)
					i += 2
					continue
				}
				if inner[i] == quote {
					i++
					break
				}
				valSb.WriteByte(inner[i])
				i++
			}
			result[key] = valSb.String()
		} else if inner[i] == '{' || inner[i] == '[' {
			openCh := inner[i]
			closeCh := byte('}')
			if openCh == '[' {
				closeCh = ']'
			}
			depth := 0
			valStart := i
			inStr := false
			var strQuote byte
			for i < n {
				ch := inner[i]
				if inStr {
					if ch == '\\' && i+1 < n {
						i += 2
						continue
					}
					if ch == strQuote {
						inStr = false
					}
				} else {
					if ch == '"' || ch == '\'' {
						inStr = true
						strQuote = ch
					} else if ch == openCh {
						depth++
					} else if ch == closeCh {
						depth--
						if depth == 0 {
							i++
							break
						}
					}
				}
				i++
			}
			rawNested := inner[valStart:i]
			var parsedNested any
			if json.Unmarshal([]byte(rawNested), &parsedNested) == nil {
				result[key] = parsedNested
			} else {
				result[key] = rawNested
			}
		} else {
			valStart := i
			for i < n && inner[i] != ',' {
				i++
			}
			rawVal := strings.TrimSpace(inner[valStart:i])
			switch strings.ToLower(rawVal) {
			case "true":
				result[key] = true
			case "false":
				result[key] = false
			case "none", "null":
				result[key] = nil
			default:
				if intVal, err := strconv.ParseInt(rawVal, 10, 64); err == nil {
					result[key] = intVal
				} else if floatVal, err := strconv.ParseFloat(rawVal, 64); err == nil {
					result[key] = floatVal
				} else {
					result[key] = rawVal
				}
			}
		}

		for i < n && (inner[i] == ' ' || inner[i] == '\t' || inner[i] == ',') {
			i++
		}
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func coerceAllToolArgs(calls []types.ToolCall, defs []types.ToolDef, projectRoot ...string) []types.ToolCall {
	if len(calls) == 0 || len(defs) == 0 {
		return calls
	}
	by := map[string]types.ToolDef{}
	for _, d := range defs {
		by[strings.ToLower(d.Name)] = d
	}
	out := calls[:0]
	for i := range calls {
		if d, ok := by[strings.ToLower(calls[i].Name)]; ok {
			args, ok := objectToolArgs(calls[i].Arguments, d)
			if !ok {
				continue
			}
			calls[i].Arguments = coerceToolArgs(args, d, projectRoot...)
		}
		out = append(out, calls[i])
	}
	return out
}

// objectToolArgs makes sure a call's arguments are a JSON object, which every
// IDE requires for tool_use input. Web models sometimes answer with a bare
// value (ReAct "Input: ls -la"): it becomes {"<key>": value} when the tool
// has exactly one required string parameter. A tool without required
// parameters gets {}. Anything else cannot be run and reports false.
func objectToolArgs(args string, def types.ToolDef) (string, bool) {
	trimmed := strings.TrimSpace(args)
	var obj map[string]any
	if json.Unmarshal([]byte(trimmed), &obj) == nil && obj != nil {
		return trimmed, true
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if len(def.InputSchema) == 0 {
		// Schema-less catalog entry (older clients): keep the historical
		// shell-style fallback.
		if trimmed == "" || trimmed == "null" {
			return "{}", true
		}
		var str string
		if json.Unmarshal([]byte(trimmed), &str) == nil {
			trimmed = str
		}
		b, _ := json.Marshal(map[string]string{"command": trimmed})
		return string(b), true
	}
	_ = json.Unmarshal(def.InputSchema, &schema)
	if len(schema.Required) == 0 {
		if trimmed == "" || trimmed == "{}" || trimmed == "null" {
			return "{}", true
		}
		return "", false
	}
	if len(schema.Required) != 1 {
		return "", false
	}
	key := schema.Required[0]
	if t, ok := schema.Properties[key].Type.(string); ok && t != "string" {
		return "", false
	}
	val := trimmed
	var str string
	if json.Unmarshal([]byte(trimmed), &str) == nil {
		val = str
	}
	if strings.TrimSpace(val) == "" {
		return "", false
	}
	b, _ := json.Marshal(map[string]string{key: val})
	return string(b), true
}

// coerceToolArgs remaps common aliases (path↔file_path, cmd↔command, content↔CodeContent,
// old↔new strings, grep/find queries) to the keys the client dialect schema expects.
func coerceToolArgs(argsJSON string, def types.ToolDef, projectRoot ...string) string {
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

	root := ""
	if len(projectRoot) > 0 {
		root = strings.TrimSpace(projectRoot[0])
	}
	resolvePath := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		if root != "" {
			return filepath.Join(root, p)
		}
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}

	toInt := func(v any) (int, bool) {
		switch val := v.(type) {
		case int:
			return val, true
		case int64:
			return int(val), true
		case float64:
			return int(val), true
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				return n, true
			}
		}
		return 0, false
	}

	wantPath := toolArgKey(def, "file_path", "path", "AbsolutePath", "TargetFile")
	remap(wantPath, "file_path", "path", "AbsolutePath", "TargetFile", "file", "filename", "filepath")

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
	remap(wantDir, "dir", "directory", "SearchDirectory", "SearchPath", "folder", "cwd")

	wantSkill := toolArgKey(def, "skill", "skill_name", "name")
	remap(wantSkill, "skill", "skill_name", "name", "skillName")

	wantWorkflow := toolArgKey(def, "name", "workflow_name", "workflow", "id")
	remap(wantWorkflow, "name", "workflow_name", "workflow", "id", "title")

	wantInputs := toolArgKey(def, "inputs", "arguments", "args", "params", "parameters")
	remap(wantInputs, "inputs", "arguments", "args", "params", "parameters")

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

	// AGY call_mcp_tool parameter normalization
	if strings.EqualFold(def.Name, "call_mcp_tool") {
		remap("ServerName", "server_name", "server", "Server", "serverName")
		remap("ToolName", "tool_name", "tool", "Tool", "toolName")
		remap("Arguments", "arguments", "args", "params", "parameters")
		if _, hasArgs := m["Arguments"]; !hasArgs {
			m["Arguments"] = map[string]any{}
			changed = true
		}
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
			if root != "" {
				m["Cwd"] = root
			} else {
				m["Cwd"] = "."
			}
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
			m["AbsolutePath"] = resolvePath(p)
			changed = true
		}
	}

	// Line range and pagination parameter translation
	if hasKey("StartLine") || hasKey("EndLine") {
		remap("StartLine", "start_line", "start", "start_line_number")
		remap("EndLine", "end_line", "end", "end_line_number")
		if _, ok := m["StartLine"]; !ok {
			if offVal, ok := m["offset"]; ok {
				if offNum, ok := toInt(offVal); ok {
					st := offNum
					if st <= 0 {
						st = 1
					}
					m["StartLine"] = st
					changed = true
					if _, hasEnd := m["EndLine"]; !hasEnd {
						if limVal, ok := m["limit"]; ok {
							if limNum, ok := toInt(limVal); ok && limNum > 0 {
								m["EndLine"] = st + limNum - 1
							}
						}
					}
				}
			}
		}
	}
	if hasKey("offset") || hasKey("limit") {
		remap("offset", "StartLine", "start_line", "start")
		if _, ok := m["limit"]; !ok {
			endKey := ""
			for _, k := range []string{"EndLine", "end_line", "end", "end_line_number"} {
				if _, ok := m[k]; ok {
					endKey = k
					break
				}
			}
			if endKey != "" {
				if endNum, ok := toInt(m[endKey]); ok {
					st := 1
					if stVal, ok := m["offset"]; ok {
						if sNum, ok := toInt(stVal); ok {
							st = sNum
						}
					}
					if endNum >= st {
						m["limit"] = endNum - st + 1
						delete(m, endKey)
						changed = true
					}
				}
			}
		}
	}

	// Inspect file on disk to determine actual line bounds and TargetContent location
	var fileLines int
	var foundStart, foundEnd int
	checkPath := ""
	if p, ok := m["TargetFile"].(string); ok && p != "" {
		checkPath = resolvePath(p)
	} else if p, ok := m["AbsolutePath"].(string); ok && p != "" {
		checkPath = resolvePath(p)
	} else if p, ok := m["file_path"].(string); ok && p != "" {
		checkPath = resolvePath(p)
	} else if p, ok := m["path"].(string); ok && p != "" {
		checkPath = resolvePath(p)
	}
	if checkPath != "" {
		if contentBytes, err := os.ReadFile(checkPath); err == nil {
			lines := strings.Split(string(contentBytes), "\n")
			fileLines = len(lines)
			if targetStr, ok := m["TargetContent"].(string); ok && targetStr != "" {
				targetLines := strings.Split(targetStr, "\n")
				tLen := len(targetLines)
				// 1. Exact match
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
				// 2. Whitespace-trimmed line fallback match
				if foundStart == 0 {
					for i := 0; i <= len(lines)-tLen; i++ {
						match := true
						for j := 0; j < tLen; j++ {
							if strings.TrimRight(lines[i+j], "\r \t") != strings.TrimRight(targetLines[j], "\r \t") {
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
			st := 1
			if sVal, ok := toInt(m["StartLine"]); ok && sVal > 0 {
				st = sVal
			}
			maxEnd := st + 799
			if fileLines > 0 && fileLines < maxEnd {
				m["EndLine"] = fileLines
			} else {
				m["EndLine"] = maxEnd
			}
			changed = true
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["EndLine"] = n
				changed = true
			}
		}
		// Clamp to at most StartLine + 799 (max 800 lines limit)
		st := 1
		if sVal, ok := toInt(m["StartLine"]); ok && sVal > 0 {
			st = sVal
		}
		maxEnd := st + 799
		if curEnd, ok := toInt(m["EndLine"]); ok && curEnd > maxEnd {
			m["EndLine"] = maxEnd
			changed = true
		}
		if fileLines > 0 {
			if curEnd, ok := toInt(m["EndLine"]); ok && curEnd > fileLines {
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

	if dropInvalidOptionalArgs(m, def.InputSchema) {
		changed = true
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

// dropInvalidOptionalArgs removes optional arguments a web model filled with a
// value the schema does not allow (null, or outside the enum). Web models fill
// every catalog key — Agent {"isolation":"none","model":"claude-haiku-…"} —
// and the client rejects the whole call, while omitting them runs it.
func dropInvalidOptionalArgs(m map[string]any, schemaRaw json.RawMessage) bool {
	if len(schemaRaw) == 0 {
		return false
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Enum []any `json:"enum"`
		} `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &schema) != nil {
		return false
	}
	required := map[string]bool{}
	for _, k := range schema.Required {
		required[k] = true
	}
	dropped := false
	for k, v := range m {
		if required[k] {
			continue
		}
		if v == nil {
			delete(m, k)
			dropped = true
			continue
		}
		prop, ok := schema.Properties[k]
		if !ok || len(prop.Enum) == 0 {
			continue
		}
		allowed := false
		for _, e := range prop.Enum {
			if reflect.DeepEqual(e, v) || fmt.Sprint(e) == fmt.Sprint(v) {
				allowed = true
				break
			}
		}
		if !allowed {
			delete(m, k)
			dropped = true
		}
	}
	return dropped
}

func parseToolCallJSON(raw string) (name, id, args string, ok bool) {
	raw = jsonrepair.StripMarkdownFences(raw)
	raw = strings.TrimSpace(raw)
	raw = reTrailComma.ReplaceAllString(raw, "$1")

	extractFromMap := func(m map[string]json.RawMessage) (string, string, string, bool) {
		var n, i, a string
		// 1. Extract name (name, Name, action, tool, or function.name)
		for _, k := range []string{"name", "Name", "action", "tool"} {
			if v, found := m[k]; found {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					n = s
					break
				}
			}
		}
		if n == "" {
			if fnRaw, found := m["function"]; found {
				var fnObj map[string]json.RawMessage
				if json.Unmarshal(fnRaw, &fnObj) == nil {
					for _, k := range []string{"name", "Name"} {
						if v, f := fnObj[k]; f {
							var s string
							if json.Unmarshal(v, &s) == nil && s != "" {
								n = s
								break
							}
						}
					}
					if fnArgs, f := fnObj["arguments"]; f {
						a = string(fnArgs)
					}
				}
			}
		}
		if n == "" {
			return "", "", "", false
		}

		// 2. Extract id
		for _, k := range []string{"id", "ID", "tool_call_id"} {
			if v, found := m[k]; found {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					i = s
					break
				}
			}
		}

		// 3. Extract arguments / parameters / input
		if a == "" {
			for _, k := range []string{"arguments", "Arguments", "parameters", "Parameters", "input", "Input", "action_input", "tool_input"} {
				if v, found := m[k]; found && len(v) > 0 && string(v) != "null" {
					a = string(v)
					break
				}
			}
		}

		// If arguments was a stringified JSON string (e.g. "\"{\\\"command\\\": ...}\"")
		if strings.HasPrefix(strings.TrimSpace(a), `"`) {
			var unquoted string
			if json.Unmarshal([]byte(a), &unquoted) == nil && json.Valid([]byte(unquoted)) {
				a = unquoted
			}
		}

		// 4. If a is still empty or "{}", check for flat arguments: e.g. {"name": "Bash", "command": "git status"}
		if a == "" || strings.TrimSpace(a) == "{}" {
			remaining := make(map[string]any)
			for k, v := range m {
				lowerK := strings.ToLower(k)
				if lowerK == "name" || lowerK == "id" || lowerK == "type" ||
					lowerK == "action" || lowerK == "tool" || lowerK == "thought" ||
					lowerK == "function" || lowerK == "explanation" || lowerK == "reasoning" {
					continue
				}
				var parsedVal any
				if json.Unmarshal(v, &parsedVal) == nil {
					remaining[k] = parsedVal
				} else {
					remaining[k] = string(v)
				}
			}
			if len(remaining) > 0 {
				if b, err := json.Marshal(remaining); err == nil {
					a = string(b)
				}
			}
		}

		if a == "" {
			a = "{}"
		}
		return n, i, a, true
	}

	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &m) == nil {
		if n, i, a, found := extractFromMap(m); found {
			return n, i, a, true
		}
	}

	repaired := jsonrepair.Repair(raw)
	repaired = reTrailComma.ReplaceAllString(repaired, "$1")
	if json.Unmarshal([]byte(repaired), &m) == nil {
		if n, i, a, found := extractFromMap(m); found {
			return n, i, a, true
		}
	}

	// Fallback regex extraction
	reName := regexp.MustCompile(`"(?:name|Name|action|tool)"\s*:\s*"([^"]+)"`)
	if match := reName.FindStringSubmatch(raw); len(match) > 1 {
		name = match[1]
		reID := regexp.MustCompile(`"(?:id|ID)"\s*:\s*"([^"]+)"`)
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

// StripWebToolMarkup removes protocol / bash fences / thought tags so Claude Code does not
// also print the commands or thinking internal steps as raw assistant text.
func StripWebToolMarkup(text string) string {
	s := reThought.ReplaceAllString(text, "")
	s = reThinking.ReplaceAllString(s, "")
	s = reReflection.ReplaceAllString(s, "")
	s = reXMLTool.ReplaceAllString(s, "")
	s = reHyphenTool.ReplaceAllString(s, "")
	s = reInvokeTool.ReplaceAllString(s, "")
	s = reAMUXTool.ReplaceAllString(s, "")
	s = reBracketTool.ReplaceAllString(s, "")
	s = reBracketToolAlt.ReplaceAllString(s, "")
	s = reStrayBracketTool.ReplaceAllString(s, "")
	s = reGeminiCall.ReplaceAllString(s, "")
	s = reEndNotice.ReplaceAllString(s, "")
	s = reProtocolEcho.ReplaceAllString(s, "")
	s = reXferNotice.ReplaceAllString(s, "")
	s = reCatalogNotice.ReplaceAllString(s, "")
	s = reToolsLiveNotice.ReplaceAllString(s, "")
	s = reToolResultMarker.ReplaceAllString(s, "")
	s = reToolCallFence.ReplaceAllString(s, "")
	s = reToolJSON.ReplaceAllString(s, "")
	s = reBashFence.ReplaceAllString(s, "")
	s = reEmptyFence.ReplaceAllString(s, "")
	s = reActionTool.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// StripInternalThoughtAndToolTags removes thinking tags (<thought>, <thinking>, <reflection>)
// and tool markup tags (<tool_call>, [tool_call], <invoke>, call:default_api:, etc.) while strictly preserving
// legitimate user-facing markdown code blocks (e.g. ```bash, ```json, ```go) in conversational output.
func StripInternalThoughtAndToolTags(text string) string {
	s := reThought.ReplaceAllString(text, "")
	s = reThinking.ReplaceAllString(s, "")
	s = reReflection.ReplaceAllString(s, "")
	s = reXMLTool.ReplaceAllString(s, "")
	s = reHyphenTool.ReplaceAllString(s, "")
	s = reInvokeTool.ReplaceAllString(s, "")
	s = reAMUXTool.ReplaceAllString(s, "")
	s = reBracketTool.ReplaceAllString(s, "")
	s = reBracketToolAlt.ReplaceAllString(s, "")
	s = reStrayBracketTool.ReplaceAllString(s, "")
	s = reGeminiCall.ReplaceAllString(s, "")
	s = reActionTool.ReplaceAllString(s, "")
	s = reEndNotice.ReplaceAllString(s, "")
	s = reProtocolEcho.ReplaceAllString(s, "")
	s = reXferNotice.ReplaceAllString(s, "")
	s = reCatalogNotice.ReplaceAllString(s, "")
	s = reToolsLiveNotice.ReplaceAllString(s, "")
	s = reToolResultMarker.ReplaceAllString(s, "")
	s = reToolCallFence.ReplaceAllString(s, "")
	s = reEmptyFence.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
