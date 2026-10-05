## MODIFIED Requirements

### Requirement: Enhanced fuzzy JSON argument recovery
The web tool parser SHALL recover from common web model JSON formatting defects including unescaped quotes within bash arguments, unescaped newlines in command bodies, trailing commas, single-item array wrappers, and truncated syntax via AST repair.

#### Scenario: Bash command with unescaped double quotes
- **WHEN** a web model generates `<tool_call>{"name":"Bash","arguments":{"command":"git commit -m "fix: resolve bug""}}</tool_call>`
- **THEN** the parser successfully repairs the inner quotes and emits a valid `Bash` tool call with command `git commit -m "fix: resolve bug"`

## ADDED Requirements

### Requirement: Two-phase thinking and tool call separation
The prompt generator SHALL enforce distinct demarcation tags (`<thought>...</thought>` vs `<tool_call>...</tool_call>`) so that model reasoning text is never erroneously parsed as tool parameters.

#### Scenario: Thought stream separation
- **WHEN** a web model outputs chain-of-thought analysis containing code snippets alongside tool invocations
- **THEN** only the demarcated `<tool_call>` payload is parsed for execution, and thought content is preserved for streaming
