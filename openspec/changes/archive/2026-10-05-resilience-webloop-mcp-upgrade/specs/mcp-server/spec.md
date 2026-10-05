## ADDED Requirements

### Requirement: Workspace context parameter in amux_ask
The `amux_ask` MCP tool SHALL accept an optional `context` parameter containing project files or workspace status to be prepended to the user prompt.

#### Scenario: Ask with workspace context
- **WHEN** an MCP client calls `amux_ask` with `prompt: "Review this function"` and `context: "File: foo.go\npackage foo..."`
- **THEN** the request to the pool includes the context prepended to the prompt
