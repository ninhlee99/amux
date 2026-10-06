## Why

A critical review from an unforgiving developer reveals serious friction and reliability flaws in AMUX:
1. Multi-turn tool execution across Web accounts (ChatGPT Web, Claude Web, Gemini Web) frequently breaks, triggers rate-limits, or leaks internal prompt markup into user terminal interfaces due to naive full-context recreation on every single tool step and leaky regex sanitation.
2. OpenAI streaming protocol in `pkg/bridge/openai.go` prematurely flushes raw tool markup as assistant text to Cursor / Windsurf / Zed / Cline clients before parsing `tool_calls`, causing broken or duplicate UI messages.
3. Web sessions on Claude Web create new chat conversations every single tool step (5 tool steps = 5 new conversations on claude.ai), rapidly hitting Cloudflare rate limits and quota ceilings.
4. CLI UX has friction: `amux doctor` misses detection for modern tools (Windsurf.app bundle, aider, opencode), `amux run` lacks automated settings configuration for Cursor/Windsurf GUI AI Chat, and 503 error messages fail to provide actionable remediation.

Fixing these issues transforms AMUX into a truly delightful, rock-solid AI Gateway that flawlessly supports any coding IDE agent across Web, Subscription, and API backends.

## What Changes

- **WebLoop & Multi-Turn Tool Loop**:
  - Fix tool delta synthesis in `BuildDeltaWebPrompt` and `WebBackendPrompt` to recognize trailing `tool` results and preserve server-side web conversation threads instead of discarding them.
  - Eliminate the "create new conversation every tool step" bug on Claude Web (`claude_web.go`), ChatGPT Web (`chatgpt_web.go`), and Gemini Web (`gemini_web.go`), saving 80-90% token overhead and preventing 429 rate limit errors.
  - Strengthen `StripWebToolMarkup` and parser regexes to completely eliminate leaked internal prompt tags (`[end] Need data`, `<thought>`, `<reflection>`, `[xfer] Continue`, `[Tool result]:`, `<function_call>`, `<tool-call>`).
- **OpenAI Bridge Tool Call Streaming**:
  - Buffer potential tool markup during streaming in `pkg/bridge/openai.go` so raw `<tool_call>` markup is NEVER prematurely flushed to Cursor/OpenAI client chat interfaces.
- **MCP Server & Tool Parity for Web Accounts**:
  - Enhance `amux_ask`, `amux_review`, `amux_diagnose`, `amux_fix`, `amux_analyze` in `pkg/mcp/tools.go` and `pkg/mcp/backend.go` with automatic sanitization and transient web session retry.
  - Expand `amux mcp install` to auto-detect and support Cline, Roo Code, Windsurf, Zed, and VS Code smoothly.
- **IDE Runner & Pre-flight Diagnostics**:
  - Enhance `amux doctor` to detect applications installed in `/Applications` (Windsurf.app, Cursor.app, Zed.app), check CLI tools (`aider`, `opencode`), and verify web accounts health.
  - Provide `--setup` flag for `amux run cursor` and `amux run windsurf` to automatically wire settings without requiring manual GUI tweaking.
  - Enhance gateway 503 / 429 error messages with clear, actionable CLI command suggestions.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: Fix tool delta synthesis for multi-turn tool loops, eliminate conversational prompt leakages, and ensure lossless lenient parsing for all web model dialects.
- `ide-integration`: Enhance IDE detection for macOS app bundles and agent CLIs (`aider`, `opencode`, `windsurf`), provide auto-configuration for Cursor/Windsurf GUI settings in `amux run`, and refine diagnostic error reporting.
- `mcp-server`: Improve MCP tool resilience with Web accounts, add retry on transient web cookie/auth challenges, and expand MCP install targets.

## Impact

- `pkg/provider/prompt.go`, `pkg/provider/claude_web.go`, `pkg/provider/chatgpt_web.go`, `pkg/provider/gemini_web.go`
- `pkg/tools/webloop.go`
- `pkg/bridge/openai.go`
- `pkg/mcp/backend.go`, `pkg/mcp/tools.go`, `pkg/mcp/install.go`
- `pkg/cli/run.go`, `pkg/cli/doctor.go`, `pkg/hook/cursor.go`
- Unit and integration tests in `pkg/tools/`, `pkg/provider/`, `pkg/bridge/`, `pkg/mcp/`, `pkg/cli/`
