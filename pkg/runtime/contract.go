package runtime

import (
	"fmt"
	"sort"
	"strings"
)

// BuildRuntimeContract generates the strict contract prompt including the live schema catalog.
func BuildRuntimeContract(m *RuntimeManifest) string {
	if m == nil || len(m.Tools) == 0 {
		return ""
	}
	runtimeName := m.Runtime
	if runtimeName == "" {
		runtimeName = "NativeRuntime"
	}

	var sb strings.Builder
	sb.WriteString("Coding-agent backend. Client executes tools locally with automated schema validation. [Tool result] = verified output.\n")
	sb.WriteString(fmt.Sprintf("================================================================================\n"))
	sb.WriteString(fmt.Sprintf("STRICT RUNTIME CONTRACT: Host Environment is [%s]\n", runtimeName))
	sb.WriteString(fmt.Sprintf("================================================================================\n"))
	sb.WriteString(`STRICT EXECUTION RULES - ZERO TOLERANCE:
1. EXCLUSIVE CATALOG USAGE - NO TOOL HALLUCINATIONS:
   - ONLY tools listed in the CATALOG below exist in this environment.
   - NEVER call tools from a different IDE or CLI.
     * If host is Antigravity: NEVER call Claude's 'Bash', 'FileEdit', or 'FileRead'. You MUST use 'run_command', 'replace_file_content', 'view_file'.
     * If host is Claude Code: NEVER call 'run_command' or 'replace_file_content'. You MUST use 'Bash', 'FileEdit', etc.
     * If host is Cursor: NEVER call tools not in Cursor's catalog.
   - NEVER invent, synthesize, or hallucinate tool names. Calling an unlisted tool causes an immediate FATAL failure.
   - If a tool is not in the CATALOG, it DOES NOT EXIST.

2. STRICT PARAMETER & SCHEMA COMPLIANCE:
   - Every parameter name, casing, and type MUST match the schema EXACTLY.
   - You MUST supply ALL required properties listed for each tool.
   - Do NOT rename properties (e.g. if catalog lists 'CommandLine', use 'CommandLine' — NEVER rename to 'command' or 'cmd').
   - Do NOT add undeclared, unsupported, or extra properties.

3. IMMEDIATE TOOL EXECUTION:
   - Need terminal execution, file inspection, search, or skill context (/open-pr:review, /open-pr:fix) → emit <tool_call> immediately.
   - Never claim inability, lack of runtime, missing files, or ask the user to paste diffs.

4. FORMAT:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Multiple blocks OK.

CATALOG
`)
	sb.WriteString(FormatSchemaCatalog(m))
	return sb.String()
}

// FormatSchemaCatalog renders all tools in the manifest into a high-fidelity catalog.
func FormatSchemaCatalog(m *RuntimeManifest) string {
	if m == nil || len(m.Tools) == 0 {
		return ""
	}

	sortedTools := make([]NativeToolDefinition, len(m.Tools))
	copy(sortedTools, m.Tools)
	sort.Slice(sortedTools, func(i, j int) bool {
		return sortedTools[i].Name < sortedTools[j].Name
	})

	var sb strings.Builder
	for _, t := range sortedTools {
		sb.WriteString(FormatToolSchema(t))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// FormatToolSchema formats a single tool definition with clear required/optional parameters.
// Also includes the compact signature `ToolName:key:type` for backward-compatible test assertions.
func FormatToolSchema(t NativeToolDefinition) string {
	var compactParts []string
	var reqParts []string
	var optParts []string

	for _, p := range t.Parameters {
		pair := fmt.Sprintf("%s:%s", p.Name, p.Type)
		compactParts = append(compactParts, pair)
		if p.Required {
			reqParts = append(reqParts, pair)
		} else {
			optParts = append(optParts, pair)
		}
	}

	// Example: "Read:file_path:string,offset:number"
	compactSig := t.Name
	if len(compactParts) > 0 {
		compactSig = fmt.Sprintf("%s:%s", t.Name, strings.Join(compactParts, ","))
	}

	var sb strings.Builder
	sb.WriteString(compactSig)
	if len(reqParts) > 0 {
		sb.WriteString(" | required: [")
		sb.WriteString(strings.Join(reqParts, ", "))
		sb.WriteString("]")
	}
	if len(optParts) > 0 {
		sb.WriteString(" | optional: [")
		sb.WriteString(strings.Join(optParts, ", "))
		sb.WriteString("]")
	}

	return sb.String()
}
