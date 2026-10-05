## Why

Coding agents and developers need specialized tools beyond generic QA and diff reviews. Adding dedicated tools for bug diagnosis, code fixing, and architectural analysis transforms AMUX's MCP server into a full-spectrum software development assistant.

## What Changes

- Register `amux_diagnose` MCP tool for error/stack trace investigation and root cause analysis.
- Register `amux_fix` MCP tool for targeted code patch generation.
- Register `amux_analyze` MCP tool for project structure and architecture evaluation.

## Capabilities

### Modified Capabilities
- `mcp-server`: Add `amux_diagnose`, `amux_fix`, and `amux_analyze` tools to the pool tools suite.

## Impact

- `pkg/mcp/tools.go`: Register new tools with domain-specific system prompts.
- `pkg/mcp/server_test.go`: Add test cases for all new MCP tools.
