## MODIFIED Requirements

### Requirement: Web tool-call emulation
For text-only backends the gateway SHALL inject a compact tool catalog and 1-shot example into the prompt and parse `<tool_call>{json}</tool_call>` (and tolerated structured variants including `[tool_call]`, `<<<AMUX_TOOL>>>`, and markdown fences labeled `tool_call` or `json` with explicit tool properties) from the reply into native tool calls, coercing arguments to the tool schema while avoiding fictitious tool call generation from natural language refusal prose.

#### Scenario: Web reply with a tool call
- **WHEN** ChatGPT web answers with a `<tool_call>` block naming `Read`
- **THEN** the client receives a structured `Read` tool call, not the raw markup

#### Scenario: Web reply with refusal prose
- **WHEN** a web model responds with conversational refusal without any tool markup
- **THEN** the client receives the clean assistant text response without fictitious forced tool calls
