# tool-dialects Specification

## Purpose
Tools always execute on the client (Claude Code, Codex, Antigravity, Cursor); amux only translates tool definitions, calls and results between dialects, and emulates tool calling for text-only web backends.
## Requirements
### Requirement: Client-side execution only
The gateway MUST NOT execute shell commands or file operations requested by a model; it SHALL return tool calls to the client in the client's dialect and accept tool results back.

#### Scenario: Bash tool call
- **WHEN** a backend asks to run `Bash` with `{"command":"ls"}`
- **THEN** Claude Code receives a `tool_use` block and runs it locally

### Requirement: Cross-dialect tool translation
Tool names and arguments SHALL be translated between Claude (`Bash`, `Read`, `mcp__server__tool`), Codex (`exec_command`, Responses function calls), Antigravity (`run_command`, `call_mcp_tool`) and Cursor/OpenAI shapes, preserving JSON Schemas.

#### Scenario: MCP tool from an Antigravity-style call
- **WHEN** a backend emits `call_mcp_tool` with server `github` and tool `create_issue`
- **THEN** a Claude client receives `mcp__github__create_issue` with the same arguments

### Requirement: Web tool-call emulation
For text-only backends the gateway SHALL inject a compact tool catalog and 1-shot example into the prompt and parse `<tool_call>{json}</tool_call>` (and tolerated structured variants including `[tool_call]`, `<<<AMUX_TOOL>>>`, and markdown fences labeled `tool_call` or `json` with explicit tool properties) from the reply into native tool calls, coercing arguments to the tool schema while avoiding fictitious tool call generation from natural language refusal prose.

#### Scenario: Web reply with a tool call
- **WHEN** ChatGPT web answers with a `<tool_call>` block naming `Read`
- **THEN** the client receives a structured `Read` tool call, not the raw markup

#### Scenario: Web reply with refusal prose
- **WHEN** a web model responds with conversational refusal without any tool markup
- **THEN** the client receives the clean assistant text response without fictitious forced tool calls

