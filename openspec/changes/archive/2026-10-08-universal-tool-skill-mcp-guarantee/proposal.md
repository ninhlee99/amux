## Why

The user requires 100% reliability for all tools, skills (e.g., `/open-pr:review`, `/open-pr:fix`, `/commit`, custom skills), and MCP servers across every adapter in AMUX, regardless of project age, session state, or account type. Currently, IDE metadata blocks such as trailing `<system-reminder>... (no content)` confuse turn classification, mistaking mid-loop turns for new user requests and dropping the original task goal. Additionally, when web models output executable commands in markdown code fences or inline backticks without explicit `<tool_call>` tags, the loop can stall.

We need comprehensive hardening across prompt engineering, turn classification, and self-healing tool call recovery to guarantee 100% execution parity.

## What Changes

- **Skill & Reminder Context Normalization**: Filter IDE `<system-reminder>` wrappers in `cleanUserTurnContent` so that `endsWithUserRequest`, `currentUserTask`, and `hasUserTurn` accurately distinguish genuine user task inputs from client metadata updates, preserving ongoing skill tool loops.
- **Multi-Tier Tool Recovery**: In `FinalizeWebToolCalls`, if no `<tool_call>` markup is found, recover executable commands from markdown `bash`/`sh` fences and inline backtick commands.
- **Skill Intent Kickstart**: For explicit skill requests (e.g. `/open-pr:*`, PR inspection) where a web model produces purely conversational prose or refusal, auto-initiate repository status checking (`Bash: git status`) to supply real execution results and pull the model into the active loop.
- **Universal MCP Normalization**: Verify and preserve MCP schemas (`mcp__*`, `call_mcp_tool`) with robust case-insensitive argument mapping.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: Add skill turn classification normalization and multi-tier command recovery.
- `webloop-self-healing`: Guarantee self-healing tool call recovery when web models output markdown fences, backtick commands, or conversational prose on skill tasks.

## Impact

- `pkg/provider/prompt.go`: Add `cleanUserTurnContent`, update `currentUserTask`, `hasUserTurn`, `endsWithUserRequest`.
- `pkg/tools/webloop.go`: Upgrade `FinalizeWebToolCalls` to recover markdown bash fences and backtick commands when zero tool calls exist.
- Unit and matrix tests: Add comprehensive tests for skill multi-turn persistence, markdown fence recovery, and MCP tool translation.
