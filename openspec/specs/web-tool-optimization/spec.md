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
When formatting requests with tools for web chat sessions, the gateway SHALL include a concise 1-shot example demonstrating tool calling markup syntax to guide the web model, and SHALL use neutral, non-adversarial protocol framing without coercive phrases (such as "never refuse", "never claim lack of tools", "DO NOT decline", "fake edits are forbidden") that trigger model safety refusals.

#### Scenario: Tool catalog and few-shot formatting
- **WHEN** a client sends a ChatRequest containing tools to a Web backend
- **THEN** the injected prompt includes both the catalog and a 1-shot `<tool_call>` example using neutral technical protocol instructions

### Requirement: Robust web refusal detection without leaking simulation debate
The gateway SHALL detect web model refusal or simulation concerns (including indications of text simulation, harness distrust, or model inability to access tools) in `IsToolRefusal`, preventing raw refusal essays from degrading the user experience while ensuring valid tool calls and normal responses are preserved across Claude, ChatGPT, and Gemini web providers.

#### Scenario: Claude Web simulation refusal detected
- **WHEN** a web model response includes statements rejecting the session as a text simulation or expressing prompt injection suspicion
- **THEN** `IsToolRefusal` evaluates to true, allowing the gateway to handle the refusal appropriately

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

### Requirement: Live thinking streaming for web accounts
`wrapWebStream` SHALL extract `<thought>` and `<thinking>` tags in real-time as chunks arrive from web accounts and stream them as `Thinking` deltas, eliminating perceived freezing in client IDEs.

#### Scenario: Real-time thinking streaming
- **WHEN** a web account outputs `<thought>Analyzing files...</thought>`
- **THEN** the content inside the tags is streamed immediately as a thinking delta instead of being buffered until completion

### Requirement: Developer-grade tool output truncation
`PruneToolResult` SHALL support tool output payloads up to 24,000 bytes and 300 lines before truncating, ensuring typical source code files are not truncated during agent tool execution.

#### Scenario: Reading a 150-line code file
- **WHEN** a tool returns 150 lines of code content under 24KB
- **THEN** `PruneToolResult` returns the full content without omitting lines in the middle

### Requirement: Provider-specific web tool prompt specialization
The gateway SHALL tailor web tool prompt preambles, reminders, and closers specifically to the active web provider:
- For Claude Web (`claude`): The prompt SHALL instruct the model neutrally on tool invocation without referencing Python or container sandboxes.
- For ChatGPT Web (`chatgpt`): The prompt SHALL advise the model that internal Python/container tools cannot access the workspace, directing execution to `<tool_call>`.
- For Gemini Web (`gemini`): The prompt SHALL provide workspace tool directives aligned with Google model formats.

#### Scenario: Claude Web prompt omits container sandbox text
- **WHEN** formatting a prompt for Claude Web
- **THEN** the preamble and reminder do not contain references to Python or container sandboxes

#### Scenario: ChatGPT Web prompt retains sandbox steering
- **WHEN** formatting a prompt for ChatGPT Web
- **THEN** the preamble and reminder advise the model that internal python/container tools cannot access the workspace

### Requirement: Skill metadata turn normalization
The gateway prompt engine SHALL filter IDE metadata blocks such as `<system-reminder>` when inspecting user messages, ensuring that trailing metadata updates containing `(no content)` are not misclassified as new user requests, and that the original user task goal (including slash commands and skills) is preserved across all subsequent tool turns.

#### Scenario: Trailing skill reminder does not break tool loop
- **WHEN** an IDE client appends a user message with `<system-reminder>15000000 tokens left</system-reminder>\n\n(no content)` following a tool execution
- **THEN** the prompt engine treats the session as an ongoing tool loop, does not inject new-request cues, and maintains the active task goal

### Requirement: Web catalog marks optional and enumerated arguments
The web tool catalog SHALL list required arguments as `name:type`, optional arguments as `name?:type`, and enumerated arguments by their allowed values joined with `|`. Before a parsed web tool call is returned to the client, optional arguments whose value is null or outside the schema enum SHALL be removed.

#### Scenario: ChatGPT fills Agent isolation with an invalid value
- **WHEN** a web reply calls `Agent` with `{"prompt":"count lines","description":"d","isolation":"none"}` and `isolation` is an optional enum of `worktree|remote`
- **THEN** the tool call returned to the client has no `isolation` argument and keeps `prompt` and `description`

### Requirement: Task cue ignores skill bodies
When the gateway restates the current task for a web model, it SHALL use the latest user turn that is not a skill expansion (a turn starting with `Base directory for this skill:`).

#### Scenario: Skill loaded mid-task
- **WHEN** the history is a user request, a `Skill` tool call, and the injected skill body
- **THEN** the restated task is the user request

### Requirement: Echoed protocol lines are not shown
Text the client receives from a web reply SHALL NOT contain lines that only repeat the gateway's tool protocol instructions, such as "Wait for [Tool result] before continuing.".

#### Scenario: Gemini echoes the wait line
- **WHEN** a Gemini Web reply is "Wait for [Tool result] before continuing." followed by a `<tool_call>`
- **THEN** the client receives the tool call and no text
