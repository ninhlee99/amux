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
The web tool parser SHALL recover from common web model JSON formatting defects including unescaped quotes within bash arguments, unescaped newlines in command bodies, trailing commas, single-item array wrappers, and truncated syntax via AST repair.

#### Scenario: Bash command with unescaped double quotes
- **WHEN** a web model generates `<tool_call>{"name":"Bash","arguments":{"command":"git commit -m "fix: resolve bug""}}</tool_call>`
- **THEN** the parser successfully repairs the inner quotes and emits a valid `Bash` tool call with command `git commit -m "fix: resolve bug"`

### Requirement: Two-phase thinking and tool call separation
The prompt generator SHALL enforce distinct demarcation tags (`<thought>...</thought>` vs `<tool_call>...</tool_call>`) so that model reasoning text is never erroneously parsed as tool parameters.

#### Scenario: Thought stream separation
- **WHEN** a web model outputs chain-of-thought analysis containing code snippets alongside tool invocations
- **THEN** only the demarcated `<tool_call>` payload is parsed for execution, and thought content is preserved for streaming

### Requirement: Multi-turn tool delta prompt preservation
When an IDE coding agent sends subsequent tool turns containing `role="tool"` results, the prompt builder SHALL isolate and emit the delta containing the recent assistant tool calls and corresponding tool results, preserving original user task context without regenerating full redundant histories.

#### Scenario: Tool result delta extraction
- **WHEN** an incoming conversation contains an initial user task followed by assistant tool invocations and subsequent tool result messages
- **THEN** `BuildDeltaWebPrompt` extracts the recent tool results and task goal, allowing the web adapter to continue without prompt duplication

### Requirement: Comprehensive web tool markup sanitization
The gateway SHALL thoroughly sanitize model output text, removing all internal protocol delimiters (`<thought>`, `</thought>`, `<tool_call>`, `</tool_call>`, `<tool-call>`, `<function_call>`, `[tool_call]`, `[end] Need data`, `[xfer] Continue`, `[Tool result]`) before emitting content to client IDEs.

#### Scenario: No leakage of internal instructions or thoughts
- **WHEN** a web model response includes thinking blocks, unclosed bracketed tool markers, or trailing protocol reminders
- **THEN** `StripWebToolMarkup` removes all markers so the client IDE receives clean text without internal artifact leakage

### Requirement: Buffered OpenAI tool streaming
When serving OpenAI/Cursor stream requests with tools configured, the gateway SHALL NOT flush raw tool call markup chunks as plain content, buffering potential tool syntax and emitting structured `tool_calls` deltas only upon validated parsing.

#### Scenario: Cursor receives clean tool call deltas
- **WHEN** a web provider streams `<tool_call>` JSON syntax to an OpenAI client
- **THEN** the bridge emits `tool_calls` SSE events instead of raw text chunks in the content stream

