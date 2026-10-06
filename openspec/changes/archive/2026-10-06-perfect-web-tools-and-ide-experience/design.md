## Context

AMUX bridges modern AI coding IDEs (Claude Code, Cursor, Windsurf, Codex, AGY, VS Code, Zed, etc.) with diverse AI backends. However, users experience friction when routing tools through Web accounts due to multi-turn conversation recreation, prompt markup leakage, and premature tool streaming in OpenAI-compatible clients. Furthermore, IDE integration and MCP setup require excessive manual configuration.

## Goals / Non-Goals

**Goals:**
- Provide rock-solid multi-turn tool support across all Web accounts (Claude Web, ChatGPT Web, Gemini Web) without creating duplicate chats or blowing up token limits.
- Guarantee clean OpenAI tool streaming in `pkg/bridge/openai.go` so Cursor / Windsurf receive proper `tool_calls` without messy raw text markup.
- Completely scrub internal prompt markers and thinking tags from assistant output.
- Make IDE launching and MCP registration effortless and fully automated with `amux doctor` and `amux run <ide> --setup`.

**Non-Goals:**
- Modifying subscription OAuth credentials flow (which already works via macOS Keychain).
- Bypassing remote rate limits that can only be handled via backoff and cooldown.

## Decisions

### 1. Robust Tool Turn Detection in `BuildDeltaWebPrompt`
In a multi-turn tool loop, client messages after turn 1 contain `role="tool"` or assistant `ToolCalls`. The prompt builder must find the latest assistant tool-call turn or trailing tool results, prepending the user's initial goal (`[Task Goal]: ...`), so the server-side conversation receives only the new delta.

### 2. Buffered Tool Call Streaming for OpenAI Bridge
In `pkg/bridge/openai.go`, when a request has `Tools`, incoming stream text from web/heuristic adapters is buffered. If it contains tool invocation markup (`<tool_call>`, `[tool_call]`, etc.), the bridge does NOT emit it as plain text chunks. Instead, it parses the calls and emits structured `tool_calls` delta events, preserving protocol integrity.

### 3. Extended Markup Scrubbing in `StripWebToolMarkup`
Add regular expressions to remove `<function_call>`, `<tool-call>`, `[end] Need data`, `[xfer] Continue`, `[Tool result]`, and stray bracketed markers from any text returned to users.

### 4. Application Bundle Inspection in `amux doctor` and `--setup` flag in `amux run`
Update `doctor.go` to inspect macOS `/Applications/` for Cursor, Windsurf, Zed, and VS Code. Add `--setup` to `amux run cursor` and `amux run windsurf` to automatically write the OpenAI base URL into the IDE's settings file.

## Risks / Trade-offs

- **Risk**: A web model might output text that looks like a tool call but isn't.
  - *Mitigation*: Fall back to text if lenient JSON parsing fails to extract valid tool names and arguments matching client definitions.
- **Risk**: Overwriting user custom settings in Cursor or Windsurf.
  - *Mitigation*: Only write settings when `--setup` is passed or with atomic JSON read-modify-write preserving all existing keys.
