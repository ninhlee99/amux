## Context

MCP hosts invoke specialized tools when specific workflows are required. Providing targeted prompts and schema contracts for diagnosis, fixing, and architecture analysis delivers significantly higher quality outputs than a generic ask tool.

## Goals / Non-Goals

**Goals:**
- Provide `amux_diagnose` with inputs for error traces, relevant code, and context.
- Provide `amux_fix` with inputs for file content, issue descriptions, and patch requirements.
- Provide `amux_analyze` with inputs for project layout/dependencies and architecture goals.

**Non-Goals:**
- Direct file modification from MCP server (the client agent remains responsible for disk writes).

## Decisions

- **Domain-tailored system prompts**: Each tool configures dedicated expert system instructions (Debugger, Senior Engineer, Principal Architect).
- **Graceful timeout defaults**: All tools default to 300s with configurable overrides.
