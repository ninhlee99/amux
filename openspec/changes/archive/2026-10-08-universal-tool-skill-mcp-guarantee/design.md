## Context

Coding assistants like Claude Code rely on complex multi-turn workflows involving Skills (e.g. `/open-pr:review`, `/open-pr:fix`), standard tools (`Bash`, `Read`, `Edit`, `Write`, `Agent`), and MCP tools (`mcp__*`, `amux_*`). When interacting with web providers (Claude Web, ChatGPT Web, Gemini Web), models can occasionally produce markdown bash code blocks, inline backtick commands, or conversational text rather than strict `<tool_call>` tags, especially when client metadata like `<system-reminder>` is present.

## Goals / Non-Goals

**Goals:**
- Guarantee 100% execution capability for tools, skills, and MCP on all adapters.
- Normalize user turns in `pkg/provider/prompt.go` by stripping `<system-reminder>` when checking for genuine user input, ensuring active skill loops persist.
- Enhance `FinalizeWebToolCalls` to fall back to markdown bash blocks and inline backtick commands if no `<tool_call>` is found.
- Kickstart tool execution when user requested an explicit skill (e.g., `/open-pr:fix`, `/open-pr:review`) but the model produced prose.
- Preserve MCP tools with robust alias and casing coercion.

**Non-Goals:**
- Interfering with normal conversational chat when no tools were requested by the client.

## Decisions

### Decision 1: `cleanUserTurnContent` in `pkg/provider/prompt.go`
Define a regex `reSystemReminder` to strip `<system-reminder>...</system-reminder>`. If the cleaned text is empty or `(no content)`, treat the turn as metadata rather than a new user goal.
- `hasUserTurn`: returns true only if substantive user prompt exists.
- `currentUserTask`: scans backwards past metadata-only turns to locate the true user request (such as `/open-pr:fix ...`).
- `endsWithUserRequest`: scans backwards past trailing metadata turns, correctly recognizing active tool loops.

### Decision 2: Multi-Tier Tool Recovery in `FinalizeWebToolCalls`
If `len(calls) == 0`:
1. Check `reBashFence` to recover shell commands from markdown codeblocks (`bash`, `sh`, `shell`).
2. Check `reBacktickCmd` to recover shell commands from inline backticks (e.g. `` `git status` ``).
3. If still empty, check if `IsToolRefusal` or the user task is an agentic skill (`/open-pr:*`, `pull/`, `review`, `fix`), and auto-kickstart with `git status` to supply real context and bring the model into the tool loop.

## Risks / Trade-offs

- [Risk] Plain conversational questions mentioning shell commands might be executed if tools are enabled.
  → *Mitigation*: Clients only pass tools when expecting the model to use tools (e.g. in coding agents). If the user asked a pure question without tools, `len(defs) == 0` and this logic never executes.
