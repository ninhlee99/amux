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

	// 1. Sort and index tools deterministically
	sortedTools := make([]NativeToolDefinition, len(m.Tools))
	copy(sortedTools, m.Tools)
	sort.Slice(sortedTools, func(i, j int) bool {
		return sortedTools[i].Name < sortedTools[j].Name
	})

	toolNames := make([]string, len(sortedTools))
	toolSet := make(map[string]bool, len(sortedTools))
	for i, t := range sortedTools {
		toolNames[i] = t.Name
		toolSet[t.Name] = true
	}

	// 2. Derive dynamic guidance strictly based on tools present in the manifest
	var dynamicGuidance []string
	if toolSet["run_command"] {
		dynamicGuidance = append(dynamicGuidance, "Execute shell commands via 'run_command' (do NOT use Claude 'Bash').")
	} else if toolSet["Bash"] {
		dynamicGuidance = append(dynamicGuidance, "Execute shell commands via 'Bash' (do NOT use Antigravity 'run_command').")
	}

	if toolSet["replace_file_content"] {
		dynamicGuidance = append(dynamicGuidance, "Edit files via 'replace_file_content' (do NOT use Claude 'FileEdit').")
	} else if toolSet["FileEdit"] {
		dynamicGuidance = append(dynamicGuidance, "Edit files via 'FileEdit' (do NOT use Antigravity 'replace_file_content').")
	} else if toolSet["Edit"] {
		dynamicGuidance = append(dynamicGuidance, "Edit files via 'Edit'.")
	}

	if toolSet["view_file"] {
		dynamicGuidance = append(dynamicGuidance, "Inspect files via 'view_file' (do NOT use Claude 'FileRead' / 'View').")
	} else if toolSet["FileRead"] {
		dynamicGuidance = append(dynamicGuidance, "Inspect files via 'FileRead' (do NOT use Antigravity 'view_file').")
	} else if toolSet["Read"] {
		dynamicGuidance = append(dynamicGuidance, "Inspect files via 'Read'.")
	}

	if toolSet["Write"] {
		dynamicGuidance = append(dynamicGuidance, "Write files via 'Write'.")
	}

	if toolSet["Workflow"] || toolSet["workflow"] {
		dynamicGuidance = append(dynamicGuidance, "Execute workflows and pipelines via 'Workflow'.")
	}

	var sb strings.Builder
	sb.WriteString("Coding-agent backend. Client executes tools locally with automated schema validation. [Tool result] = verified output.\n")
	sb.WriteString("================================================================================\n")
	sb.WriteString(fmt.Sprintf("STRICT RUNTIME CONTRACT: Host Environment is [%s]\n", runtimeName))
	sb.WriteString("================================================================================\n")
	sb.WriteString("STRICT EXECUTION RULES - ZERO TOLERANCE:\n")
	sb.WriteString("1. EXCLUSIVE CATALOG USAGE - NO TOOL HALLUCINATIONS:\n")
	sb.WriteString("   - ONLY tools listed in the CATALOG below exist in this environment.\n")
	sb.WriteString(fmt.Sprintf("   - Available executable tools: [%s].\n", strings.Join(toolNames, ", ")))
	sb.WriteString("   - NEVER call tools that are not declared in the available tools list above.\n")
	for _, g := range dynamicGuidance {
		sb.WriteString(fmt.Sprintf("   - %s\n", g))
	}
	sb.WriteString("   - NEVER invent, synthesize, or hallucinate tool names. Calling an unlisted tool causes an immediate FATAL failure.\n")
	sb.WriteString("   - If a tool is not in the CATALOG, it DOES NOT EXIST.\n")
	sb.WriteString("   - WORKFLOWS, SKILLS & EXTENSIONS:\n")
	sb.WriteString("     * High-level workflows, slash commands, and skills are EXECUTED using available terminal and file inspection tools.\n")
	sb.WriteString("     * NEVER claim a skill or workflow is missing or unavailable. Immediately emit <tool_call> using appropriate tools to inspect the workspace, run commands, and execute the requested workflow.\n\n")

	sb.WriteString(`2. STRICT PARAMETER & SCHEMA COMPLIANCE:
   - Every parameter name, casing, and type MUST match the schema EXACTLY.
   - You MUST supply ALL required properties listed for each tool.
   - Do NOT rename properties (e.g. if catalog lists 'CommandLine', use 'CommandLine' — NEVER rename to 'command' or 'cmd').
   - Do NOT add undeclared, unsupported, or extra properties.

3. IMMEDIATE TOOL EXECUTION:
   - When terminal execution, file inspection, search, or workspace context is required → emit <tool_call> immediately.
   - Never claim inability, lack of runtime, missing files, or ask the user to manually run commands.

4. FORMAT:
<tool_call>
{"name":"TOOL_NAME","arguments":{...}}
</tool_call>
Multiple blocks OK.

5. STRICT API ENGINE BEHAVIOR (ZERO WEB CHATBOT MANNERISMS):
   - You are acting strictly as an autonomous developer API engine.
   - ZERO CHATBOT FLUFF: NEVER output greetings, apologies, conversational pleasantries ("Sure!", "I'd be glad to help"), or meta-commentary ("Mình thấy...", "Tôi nhận được...").
   - ZERO HESITATION OR CLARIFICATION: NEVER ask the user what to do or whether to proceed. Autonomously execute the task to full completion immediately.
   - PURE TOOL INVOCATION: When tools or files are needed, output ONLY <tool_call> blocks. DO NOT output conversational explanations or markdown shell blocks for the user to run.
   - TRUNCATED / LARGE OUTPUTS & PERSISTED FILES: If previous tool output indicates truncated content, '<persisted-output>', or 'Full output saved to: <file>', DO NOT refuse or give up. Emit <tool_call> to inspect targeted sections or read files.

CATALOG
`)
	sb.WriteString(formatSortedCatalog(sortedTools))
	return sb.String()
}

func formatSortedCatalog(tools []NativeToolDefinition) string {
	var sb strings.Builder
	for _, t := range tools {
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
