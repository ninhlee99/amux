## Context

`amux` functions as a multi-dialect AI gateway routing requests from coding agents (Claude Code CLI, Cursor, Codex, Antigravity) to various backends (Subscriptions, Web Sessions, API Keys).
Over time, experimental modules (like Meta Muse browser driving) and ad-hoc task heuristics (`open-pr:*`, `webapp-evidence:*`) were introduced into core proxy pathways. These created code bloat, fragile dependencies on DOM selectors, and false-positive tool executions when web models answered in conversational prose.

## Goals / Non-Goals

**Goals:**
- Eliminate `pkg/muse/` and all `muse_*` dependencies from the core gateway.
- Remove hardcoded domain/task heuristics from web prompt generators and tool parsers.
- Make Web-Loop Tool Parsing (`pkg/tools/webloop.go`) deterministic and strictly bounded by explicit tool call syntax (`<tool_call>`, `<<<AMUX_CALL>>>`), preventing unintended command generation from conversational prose.
- Keep all unit tests passing 100% and ensure zero regressions across standard gateway endpoints (`/v1/messages`, `/v1/chat/completions`, `/v1/responses`, `/v1beta/models`).

**Non-Goals:**
- Retaining browser automation for `muse.ai` inside the core `amux` repository.
- Changing core subscription OAuth PKCE rotation or macOS Keychain integration.

## Decisions

### 1. Complete Removal of `pkg/muse/`
- **Rationale**: `amux` is a developer coding agent gateway. Meta Muse web automation is out of scope and introduces heavy browser session overhead and DOM flakiness.
- **Action**: Delete `pkg/muse/`, clean up `pkg/provider/muse_web.go`, `pkg/mcp/tools.go`, `pkg/cli/`.

### 2. Elimination of Hardcoded Task Intent Interceptions
- **Rationale**: Core gateway components must not inject arbitrary agent prompt phrases (`open-pr:review`, `webapp-evidence`) into generic chat requests.
- **Action**: Sanitize `BuildConcatenatedPrompt()` and `resolveUserTaskIntent()` in `pkg/provider/chatgpt_web.go` and `pkg/tools/webloop.go` to be clean, generic multi-turn prompt flatteners.

### 3. Strict Grammar & AST-like Tool Parsing in `webloop.go`
- **Rationale**: Heuristic regexes matching words like `git diff` inside markdown backticks caused the proxy to invent tool calls when the user/assistant was merely explaining git commands in chat.
- **Action**: Restrict forced tool inference only to explicit markup tags (`<tool_call>`, `[tool_call]`, `<<<AMUX_TOOL>>>`). Normal markdown code blocks are treated strictly as text responses unless explicit tool call envelope is present.

## Risks / Trade-offs

- **[Risk]** Users relying on `amux login muse` will find the command deprecated.
  - **Mitigation**: Clear error/deprecation notice or clean removal from CLI help.
- **[Risk]** Weak web models might fail to emit explicit `<tool_call>` tags when asked to run tools.
  - **Mitigation**: Provide clear, concise tool catalog definitions in the system prompt preamble so the model knows exactly how to format valid `<tool_call>` blocks.
