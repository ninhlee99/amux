package runtime

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultRuntimePromptContract is the mandatory system-level instruction injected for web models.
const DefaultRuntimePromptContract = `Coding-agent backend. Client executes tools locally with automated schema validation. [Tool result] = verified output.
STRICT RUNTIME CONTRACT:
1. Need command/file/search/PR context → emit <tool_call> immediately. Never claim inability, lack of runtime, or ask user to paste diffs.
2. For skills/plugins (/open-pr:review, /open-pr:fix): execute tools via <tool_call> to fetch PR context. You have full terminal capability.
3. STRICT SCHEMA COMPLIANCE:
   - When emitting any tool call, inspect the live CATALOG below.
   - Use the EXACT tool name and parameter names defined in the schema.
   - You MUST supply ALL required properties listed for that tool.
   - Do NOT rename properties (e.g. if the catalog lists 'CommandLine', use 'CommandLine' — do NOT rename to 'command').
   - Do NOT invent or add undeclared properties.
4. Format:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Multiple blocks OK.
`

// BuildRuntimeContract generates the full contract prompt including the live schema catalog.
func BuildRuntimeContract(m *RuntimeManifest) string {
	if m == nil || len(m.Tools) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(DefaultRuntimePromptContract)
	sb.WriteString("CATALOG\n")
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
