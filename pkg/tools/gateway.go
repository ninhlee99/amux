package tools

import (
	"encoding/json"
	"strconv"
	"strings"

	"amux-accounts/pkg/types"
)

// ToolGateway handles dynamic introspection, alias mapping, schema validation,
// default injection, and strict dialect conversion between LLM outputs (web or API)
// and coding agent clients (AGY, Claude Code, Codex, Cursor).
type ToolGateway struct{}

var defaultGateway = &ToolGateway{}

// NormalizeToolCalls applies dynamic schema-driven normalization to a slice of tool calls.
func NormalizeToolCalls(calls []types.ToolCall, defs []types.ToolDef, clientDialect string) []types.ToolCall {
	return defaultGateway.NormalizeCalls(calls, defs, clientDialect)
}

// NormalizeCalls normalizes tool calls to conform strictly to client tool definitions.
func (g *ToolGateway) NormalizeCalls(calls []types.ToolCall, defs []types.ToolDef, clientDialect string) []types.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]types.ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, g.NormalizeCall(c, defs, clientDialect))
	}
	return out
}

// NormalizeCall converts a single tool call into the exact shape expected by the client.
func (g *ToolGateway) NormalizeCall(call types.ToolCall, defs []types.ToolDef, clientDialect string) types.ToolCall {
	if len(defs) == 0 {
		return call
	}

	targetDef, found := g.findMatchingToolDef(call.Name, defs, clientDialect)
	if !found {
		// If tool name is completely unknown, return verbatim
		return call
	}

	call.Name = targetDef.Name

	// Parse arguments map
	var args map[string]any
	rawArgs := strings.TrimSpace(call.Arguments)
	if rawArgs == "" || rawArgs == "{}" {
		args = make(map[string]any)
	} else if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		// If arguments is raw string (e.g. bash command), wrap it
		args = map[string]any{"command": rawArgs}
	}

	// Apply schema-based normalization
	normalizedArgs := g.normalizeArguments(args, targetDef, clientDialect)

	b, err := json.Marshal(normalizedArgs)
	if err == nil {
		call.Arguments = string(b)
	}
	return call
}

// findMatchingToolDef locates the target ToolDef using canonical alias mapping.
func (g *ToolGateway) findMatchingToolDef(incomingName string, defs []types.ToolDef, clientDialect string) (types.ToolDef, bool) {
	lowerIncoming := strings.ToLower(strings.TrimSpace(incomingName))

	// 1. Exact match (case-insensitive)
	for _, d := range defs {
		if strings.EqualFold(d.Name, incomingName) {
			return d, true
		}
	}

	// 2. Canonical tool family matching
	by := make(map[string]types.ToolDef)
	for _, d := range defs {
		by[strings.ToLower(d.Name)] = d
	}

	switch {
	// Shell / Command execution
	case lowerIncoming == "bash" || lowerIncoming == "shell" || lowerIncoming == "run_terminal_command" ||
		lowerIncoming == "run_command" || lowerIncoming == "exec_command" || lowerIncoming == "execute_command":
		if d, ok := findToolDef(by, "run_command", "bash", "run_terminal_command", "exec_command", "execute_command", "shell"); ok {
			return d, true
		}

	// File view / read
	case lowerIncoming == "read" || lowerIncoming == "read_file" || lowerIncoming == "view_file" || lowerIncoming == "cat":
		if d, ok := findToolDef(by, "view_file", "read_file", "read"); ok {
			return d, true
		}

	// File edit / patch / replace
	case lowerIncoming == "edit" || lowerIncoming == "edit_file" || lowerIncoming == "replace_file_content" ||
		lowerIncoming == "patch" || lowerIncoming == "str_replace_editor" || lowerIncoming == "apply_patch":
		if d, ok := findToolDef(by, "replace_file_content", "edit_file", "edit", "apply_patch", "str_replace_editor"); ok {
			return d, true
		}

	// File write / create
	case lowerIncoming == "write" || lowerIncoming == "write_file" || lowerIncoming == "write_to_file" || lowerIncoming == "create_file":
		if d, ok := findToolDef(by, "write_to_file", "write_file", "write", "create_file"); ok {
			return d, true
		}

	// Grep / Code search
	case lowerIncoming == "grep" || lowerIncoming == "grep_search" || lowerIncoming == "search_code" || lowerIncoming == "search":
		if d, ok := findToolDef(by, "grep_search", "grep", "search_code", "search"); ok {
			return d, true
		}

	// Glob / File finding
	case lowerIncoming == "find" || lowerIncoming == "find_by_name" || lowerIncoming == "glob" || lowerIncoming == "file_search":
		if d, ok := findToolDef(by, "find_by_name", "glob", "find", "file_search"); ok {
			return d, true
		}

	// Subagent / Task dispatch
	case lowerIncoming == "agent" || lowerIncoming == "invoke_subagent" || lowerIncoming == "subagent" ||
		lowerIncoming == "task" || lowerIncoming == "spawn_agent" || lowerIncoming == "dispatch_agent":
		if d, ok := findToolDef(by, "invoke_subagent", "agent", "subagent", "task", "spawn_agent"); ok {
			return d, true
		}

	// Skills
	case lowerIncoming == "skill" || lowerIncoming == "load_skill" || lowerIncoming == "run_skill" || lowerIncoming == "use_skill":
		if d, ok := findToolDef(by, "skill", "load_skill", "run_skill", "use_skill"); ok {
			return d, true
		}

	// MCP calling
	case strings.HasPrefix(lowerIncoming, "mcp__") || strings.HasPrefix(lowerIncoming, "mcp_") || lowerIncoming == "call_mcp_tool":
		if d, ok := findToolDef(by, "call_mcp_tool"); ok {
			return d, true
		}
		// Try matching specific MCP tool name in client definitions
		cleanName := strings.TrimPrefix(lowerIncoming, "mcp__")
		cleanName = strings.TrimPrefix(cleanName, "mcp_")
		cleanNorm := strings.ReplaceAll(strings.ReplaceAll(cleanName, "__", "_"), "-", "_")
		for _, d := range defs {
			dClean := strings.TrimPrefix(strings.ToLower(d.Name), "mcp__")
			dClean = strings.TrimPrefix(dClean, "mcp_")
			dNorm := strings.ReplaceAll(strings.ReplaceAll(dClean, "__", "_"), "-", "_")
			if dNorm == cleanNorm || strings.HasSuffix(dNorm, "_"+cleanNorm) {
				return d, true
			}
		}
	}

	return types.ToolDef{}, false
}

// normalizeArguments performs alias lookup, stripping redundant properties,
// injecting required schema defaults, and coercing types according to def.InputSchema.
func (g *ToolGateway) normalizeArguments(m map[string]any, def types.ToolDef, clientDialect string) map[string]any {
	if m == nil {
		m = make(map[string]any)
	}

	// Parse tool schema properties and required list
	schemaProps, requiredList := g.introspectSchema(def.InputSchema)

	// Helper to check if property exists in client schema
	hasSchemaProp := func(prop string) bool {
		_, ok := schemaProps[prop]
		return ok
	}

	// Remap helper: assigns wantKey from first present altKey, then deletes altKeys
	remap := func(wantKey string, alts ...string) {
		if wantKey == "" {
			return
		}
		// If wantKey is already populated and valid, ensure alts are cleaned up to avoid 'additional properties not allowed'
		if val, exists := m[wantKey]; exists && val != nil && val != "" {
			for _, alt := range alts {
				if alt != wantKey {
					delete(m, alt)
				}
			}
			return
		}
		// Find first non-empty alt
		for _, alt := range alts {
			if v, ok := m[alt]; ok && v != nil && v != "" {
				m[wantKey] = v
				if alt != wantKey {
					delete(m, alt)
				}
				return
			}
		}
	}

	// 1. Unwrap single-element arrays (e.g. {"command": ["git status"]})
	for k, v := range m {
		if arr, ok := v.([]any); ok && len(arr) == 1 {
			if str, isStr := arr[0].(string); isStr {
				m[k] = str
			}
		}
	}

	// 2. Command execution remapping
	if hasSchemaProp("CommandLine") {
		remap("CommandLine", "CommandLine", "command", "cmd", "script", "code", "run")
	} else if hasSchemaProp("command") {
		remap("command", "command", "CommandLine", "cmd", "script", "code", "run")
	} else if hasSchemaProp("cmd") {
		remap("cmd", "cmd", "command", "CommandLine", "script", "code", "run")
	}

	// 3. File path remapping
	if hasSchemaProp("AbsolutePath") {
		remap("AbsolutePath", "AbsolutePath", "file_path", "path", "TargetFile", "SearchPath", "SearchDirectory", "file", "filename", "filepath")
	} else if hasSchemaProp("TargetFile") {
		remap("TargetFile", "TargetFile", "file_path", "path", "AbsolutePath", "file", "filename", "filepath")
	} else if hasSchemaProp("file_path") {
		remap("file_path", "file_path", "path", "AbsolutePath", "TargetFile", "file", "filename", "filepath")
	} else if hasSchemaProp("path") {
		remap("path", "path", "file_path", "AbsolutePath", "TargetFile", "file", "filename", "filepath")
	}

	// 4. File content remapping
	if hasSchemaProp("CodeContent") {
		remap("CodeContent", "CodeContent", "content", "contents", "text", "body", "code")
	} else if hasSchemaProp("content") {
		remap("content", "content", "CodeContent", "contents", "text", "body", "code")
	}

	// 5. String replacement remapping
	if hasSchemaProp("TargetContent") {
		remap("TargetContent", "TargetContent", "old_string", "old_str", "old", "orig", "original")
	} else if hasSchemaProp("old_string") {
		remap("old_string", "old_string", "TargetContent", "old_str", "old", "orig", "original")
	} else if hasSchemaProp("old_str") {
		remap("old_str", "old_str", "TargetContent", "old_string", "old", "orig", "original")
	}

	if hasSchemaProp("ReplacementContent") {
		remap("ReplacementContent", "ReplacementContent", "new_string", "new_str", "new", "replacement")
	} else if hasSchemaProp("new_string") {
		remap("new_string", "new_string", "ReplacementContent", "new_str", "new", "replacement")
	} else if hasSchemaProp("new_str") {
		remap("new_str", "new_str", "ReplacementContent", "new_string", "new", "replacement")
	}

	// 6. Search query / pattern remapping
	if hasSchemaProp("Pattern") {
		remap("Pattern", "Pattern", "query", "Query", "pattern", "glob", "regex", "search_term")
	} else if hasSchemaProp("Query") {
		remap("Query", "Query", "query", "pattern", "Pattern", "regex", "search_term")
	} else if hasSchemaProp("query") {
		remap("query", "query", "Query", "pattern", "Pattern", "regex", "search_term")
	} else if hasSchemaProp("pattern") {
		remap("pattern", "pattern", "query", "Query", "Pattern", "glob", "regex", "search_term")
	}

	// 7. Directory remapping
	if hasSchemaProp("SearchDirectory") {
		remap("SearchDirectory", "SearchDirectory", "dir", "directory", "SearchPath", "path", "cwd")
	} else if hasSchemaProp("SearchPath") {
		remap("SearchPath", "SearchPath", "dir", "directory", "SearchDirectory", "path", "cwd")
	} else if hasSchemaProp("dir") {
		remap("dir", "dir", "directory", "SearchDirectory", "SearchPath", "path", "cwd")
	}

	// 8. Skill remapping
	if hasSchemaProp("skill") {
		remap("skill", "skill", "skill_name", "name", "skillName")
	} else if hasSchemaProp("skill_name") {
		remap("skill_name", "skill_name", "skill", "name", "skillName")
	}

	// 9. Subagent Normalization
	if strings.EqualFold(def.Name, "invoke_subagent") {
		if _, hasSubs := m["Subagents"]; !hasSubs {
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
				delete(m, "type")
			}
		}
	} else if strings.EqualFold(def.Name, "agent") || strings.EqualFold(def.Name, "subagent") || strings.EqualFold(def.Name, "task") {
		// Claude/Codex client expects Agent with flat prompt/description
		if subs, ok := m["Subagents"].([]any); ok && len(subs) > 0 {
			if first, ok := subs[0].(map[string]any); ok {
				if p, ok := first["Prompt"].(string); ok && p != "" {
					m["prompt"] = p
				}
				if r, ok := first["Role"].(string); ok && r != "" {
					m["description"] = r
				}
			}
			delete(m, "Subagents")
		}
	}

	// 10. MCP Tool Normalization (call_mcp_tool vs mcp__server__tool)
	if strings.EqualFold(def.Name, "call_mcp_tool") {
		// If incoming was named mcp__server__tool or similar, extract ServerName and ToolName
		if _, hasServer := m["ServerName"]; !hasServer {
			for k, v := range m {
				if strings.HasPrefix(k, "mcp__") || strings.HasPrefix(k, "mcp_") {
					clean := strings.TrimPrefix(k, "mcp__")
					clean = strings.TrimPrefix(clean, "mcp_")
					parts := strings.SplitN(clean, "__", 2)
					if len(parts) < 2 {
						parts = strings.SplitN(clean, "_", 2)
					}
					if len(parts) == 2 {
						m["ServerName"] = parts[0]
						m["ToolName"] = parts[1]
						m["Arguments"] = v
						delete(m, k)
						break
					}
				}
			}
		}
	}

	// 11. Strict Schema Default Injection & Type Coercion for AGY / strict tools
	// Inject required defaults if missing
	if hasSchemaProp("CommandLine") {
		if _, ok := m["CommandLine"]; !ok {
			m["CommandLine"] = ""
		}
		// Strict clean-up: AGY strictly rejects 'command' as an additional property
		delete(m, "command")
		delete(m, "cmd")
	}

	if hasSchemaProp("Cwd") {
		if v, ok := m["Cwd"]; !ok || v == nil || v == "" {
			m["Cwd"] = "."
		}
	}

	if hasSchemaProp("WaitMsBeforeAsync") {
		if v, ok := m["WaitMsBeforeAsync"]; !ok || v == nil {
			m["WaitMsBeforeAsync"] = 10000
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["WaitMsBeforeAsync"] = n
			} else {
				m["WaitMsBeforeAsync"] = 10000
			}
		}
	}

	if hasSchemaProp("toolAction") {
		if v, ok := m["toolAction"]; !ok || v == nil || v == "" {
			m["toolAction"] = g.inferToolAction(def.Name)
		}
	}

	if hasSchemaProp("toolSummary") {
		if v, ok := m["toolSummary"]; !ok || v == nil || v == "" {
			m["toolSummary"] = g.inferToolSummary(def.Name)
		}
	}

	if hasSchemaProp("Instruction") {
		if v, ok := m["Instruction"]; !ok || v == nil || v == "" {
			m["Instruction"] = "Apply modifications"
		}
	}

	if hasSchemaProp("Description") {
		if v, ok := m["Description"]; !ok || v == nil || v == "" {
			m["Description"] = "Code change"
		}
	}

	if hasSchemaProp("Overwrite") {
		if v, ok := m["Overwrite"]; !ok || v == nil {
			m["Overwrite"] = true
		} else if s, isStr := v.(string); isStr {
			m["Overwrite"] = strings.EqualFold(s, "true") || s == "1"
		}
	}

	if hasSchemaProp("AllowMultiple") {
		if v, ok := m["AllowMultiple"]; !ok || v == nil {
			m["AllowMultiple"] = false
		} else if s, isStr := v.(string); isStr {
			m["AllowMultiple"] = strings.EqualFold(s, "true") || s == "1"
		}
	}

	if hasSchemaProp("StartLine") {
		if v, ok := m["StartLine"]; !ok || v == nil {
			m["StartLine"] = 1
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["StartLine"] = n
			} else {
				m["StartLine"] = 1
			}
		}
	}

	if hasSchemaProp("EndLine") {
		if v, ok := m["EndLine"]; !ok || v == nil {
			m["EndLine"] = 1000000
		} else if s, isStr := v.(string); isStr {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				m["EndLine"] = n
			} else {
				m["EndLine"] = 1000000
			}
		}
	}

	// 12. For strict required fields from schema, ensure non-nil values
	for _, req := range requiredList {
		if _, ok := m[req]; !ok {
			// Provide intelligent empty defaults according to property type
			propType := schemaProps[req]
			switch strings.ToUpper(propType) {
			case "STRING":
				m[req] = ""
			case "INTEGER", "NUMBER":
				m[req] = 0
			case "BOOLEAN":
				m[req] = false
			case "ARRAY":
				m[req] = []any{}
			case "OBJECT":
				m[req] = map[string]any{}
			}
		}
	}

	// Check if this tool definition requires stripping extraneous properties
	// E.g. AGY tools (run_command, view_file, etc.) or schemas with additionalProperties: false
	if g.isStrictClientTool(def) {
		for k := range m {
			if !hasSchemaProp(k) {
				delete(m, k)
			}
		}
	}

	return m
}

func (g *ToolGateway) isStrictClientTool(def types.ToolDef) bool {
	// If schema explicitly states additionalProperties: false
	var s struct {
		AdditionalProperties *bool `json:"additionalProperties"`
	}
	if json.Unmarshal(def.InputSchema, &s) == nil && s.AdditionalProperties != nil && !*s.AdditionalProperties {
		return true
	}
	// Known strict AGY tools
	lower := strings.ToLower(def.Name)
	if lower == "run_command" || lower == "view_file" || lower == "write_to_file" ||
		lower == "replace_file_content" || lower == "grep_search" || lower == "find_by_name" ||
		lower == "invoke_subagent" || lower == "call_mcp_tool" {
		return true
	}
	return false
}

func (g *ToolGateway) inferToolAction(toolName string) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "command") || strings.Contains(lower, "bash"):
		return "Running command"
	case strings.Contains(lower, "replace") || strings.Contains(lower, "edit") || strings.Contains(lower, "patch"):
		return "Modifying file"
	case strings.Contains(lower, "write") || strings.Contains(lower, "create"):
		return "Writing file"
	case strings.Contains(lower, "view") || strings.Contains(lower, "read"):
		return "Reading file"
	case strings.Contains(lower, "grep") || strings.Contains(lower, "find") || strings.Contains(lower, "search"):
		return "Searching code"
	case strings.Contains(lower, "subagent") || strings.Contains(lower, "agent") || strings.Contains(lower, "task"):
		return "Invoking subagent"
	case strings.Contains(lower, "mcp"):
		return "Calling MCP tool"
	default:
		return "Executing tool"
	}
}

func (g *ToolGateway) inferToolSummary(toolName string) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "command") || strings.Contains(lower, "bash"):
		return "Command execution"
	case strings.Contains(lower, "replace") || strings.Contains(lower, "edit") || strings.Contains(lower, "patch"):
		return "File modification"
	case strings.Contains(lower, "write") || strings.Contains(lower, "create"):
		return "File write"
	case strings.Contains(lower, "view") || strings.Contains(lower, "read"):
		return "File read"
	case strings.Contains(lower, "grep") || strings.Contains(lower, "find") || strings.Contains(lower, "search"):
		return "Code search"
	case strings.Contains(lower, "subagent") || strings.Contains(lower, "agent") || strings.Contains(lower, "task"):
		return "Subagent task"
	case strings.Contains(lower, "mcp"):
		return "MCP tool call"
	default:
		return "Tool execution"
	}
}

func (g *ToolGateway) introspectSchema(raw json.RawMessage) (props map[string]string, required []string) {
	props = make(map[string]string)
	if len(raw) == 0 || string(raw) == "null" {
		return props, required
	}
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return props, required
	}
	required = s.Required
	for k, rawP := range s.Properties {
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rawP, &p) == nil && p.Type != "" {
			props[k] = p.Type
		} else {
			props[k] = "any"
		}
	}
	return props, required
}
