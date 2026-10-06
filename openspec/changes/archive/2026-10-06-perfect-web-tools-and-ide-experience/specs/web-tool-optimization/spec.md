## ADDED Requirements

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
