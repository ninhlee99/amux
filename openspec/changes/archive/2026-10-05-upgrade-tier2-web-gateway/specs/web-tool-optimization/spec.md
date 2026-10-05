## ADDED Requirements

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
