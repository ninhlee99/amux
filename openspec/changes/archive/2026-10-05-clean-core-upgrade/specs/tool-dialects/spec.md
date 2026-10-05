## MODIFIED Requirements

### Requirement: Web tool-call emulation
For text-only backends the gateway SHALL inject a compact tool catalog into the prompt and parse `<tool_call>{json}</tool_call>` (and tolerated explicit variants) from the reply into native tool calls, coercing arguments to the tool schema, while strictly ignoring arbitrary prose and non-tool markdown code fences.

#### Scenario: Web reply with a tool call
- **WHEN** ChatGPT web answers with an explicit `<tool_call>` block naming `Read`
- **THEN** the client receives a structured `Read` tool call, not the raw markup

#### Scenario: Conversational prose with backtick shell commands
- **WHEN** a web model explains git syntax using regular markdown code blocks without tool call tags
- **THEN** the gateway treats it as normal text response and does not emit fake tool executions
