## ADDED Requirements

### Requirement: Enhanced fuzzy JSON argument recovery
The web tool parser SHALL recover from common web model JSON formatting defects including unescaped quotes within bash arguments, unescaped newlines in command bodies, trailing commas, and single-item array wrappers.

#### Scenario: Bash command with unescaped double quotes
- **WHEN** a web model generates `<tool_call>{"name":"Bash","arguments":{"command":"git commit -m "fix: resolve bug""}}</tool_call>`
- **THEN** the parser successfully repairs the inner quotes and emits a valid `Bash` tool call with command `git commit -m "fix: resolve bug"`
