## ADDED Requirements

### Requirement: Multi-file code review tool
The MCP server SHALL expose an `amux_review` tool that accepts a git diff or code snippet, optional review focus, and optional workspace context, routing the analysis prompt through the amux account pool.

#### Scenario: Code review across diff
- **WHEN** an MCP client calls `amux_review` with a unified git diff and focus `security`
- **THEN** amux sends a structured review prompt through the pool and returns comprehensive findings with line annotations
