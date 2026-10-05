# web-tool-optimization Specification

## Purpose
TBD - created by archiving change upgrade-tier2-web-gateway. Update Purpose after archive.
## Requirements
### Requirement: Smart tool result pruning
The gateway SHALL prune overly long tool results (exceeding 4KB or 100 lines) when flattening multi-turn message history for Web providers, preserving the leading 30 lines and trailing 40 lines with a truncation summary indicator.

#### Scenario: Large CLI output pruning
- **WHEN** an IDE client sends a `tool_result` message containing 500 lines of test output to a Web provider
- **THEN** the flattened prompt contains the first 30 lines, a truncation marker `[... truncated X lines ...]`, and the last 40 lines

### Requirement: Dynamic micro few-shot prompt injection
When formatting requests with tools for web chat sessions, the gateway SHALL include a concise 1-shot example demonstrating tool calling markup syntax to guide the web model.

#### Scenario: Tool catalog and few-shot formatting
- **WHEN** a client sends a ChatRequest containing tools to a Web backend
- **THEN** the injected prompt includes both the catalog and a 1-shot `<tool_call>` example

### Requirement: Elimination of synthetic refusal tool injection
The gateway SHALL NOT inject synthetic or heuristic tool calls when a web model outputs conversational refusal, incomplete work notices, or plain explanatory prose.

#### Scenario: Conversational response without tool markup
- **WHEN** a web model generates plain text explaining limitations or reviewing code without `<tool_call>` markup
- **THEN** the gateway streams the text verbatim to the client without synthesizing unprompted tool calls

### Requirement: Enhanced fuzzy JSON argument recovery
The web tool parser SHALL recover from common web model JSON formatting defects including unescaped quotes within bash arguments, unescaped newlines in command bodies, trailing commas, and single-item array wrappers.

#### Scenario: Bash command with unescaped double quotes
- **WHEN** a web model generates `<tool_call>{"name":"Bash","arguments":{"command":"git commit -m "fix: resolve bug""}}</tool_call>`
- **THEN** the parser successfully repairs the inner quotes and emits a valid `Bash` tool call with command `git commit -m "fix: resolve bug"`


