## ADDED Requirements

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

