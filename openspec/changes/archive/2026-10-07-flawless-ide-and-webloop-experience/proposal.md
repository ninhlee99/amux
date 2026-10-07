## Why

A rigorous, unforgiving evaluation of AMUX from the perspective of an experienced developer reveals several critical usability, reliability, and UX shortcomings:
1. **Background Daemon Black Hole**: `gateway.Start()` dispatches detached child processes with `Stdout=nil, Stderr=nil`, dropping all daemon output to `/dev/null`. `pkg/telemetry/logger.go` hardcodes legacy `~/.am` and `InitLogging` was never even called. When the background gateway encounters runtime errors or crashes, `~/.amux/gateway.log` does not exist, leaving the user with zero observability.
2. **Sandbox Pollution in `amux run`**: `amux run` promises a "Zero-Pollution Sandbox", but `amux run cursor`, `amux run windsurf`, and `amux run codex` silently mutate the developer's persistent configuration files (`settings.json`, `config.toml`) on disk without the `--setup` flag and never revert them on exit. Stopping amux breaks native IDE AI chats because they permanently point to dead `:8787`.
3. **No Per-Command Provider Routing**: Developers cannot run e.g. `amux run claude --provider gemini-web` or `amux run cursor -p claude-web`. The gateway only accepted `X-Provider` headers (which IDEs like Claude Code and Cursor cannot emit). Lack of path-based or query-based provider routing prevents sandboxed runs from pinning specific web or API accounts.
4. **Blind Web Tool Calling**: In `pkg/tools/webloop.go`, `catalogLine` strips all tool descriptions, sending only `Name:key:type,...`. Web models (ChatGPT Web, Gemini Web, Claude Web) have no semantic context on what MCP or custom tools do, leading to tool call failures and argument hallucinations.
5. **Premature Tool Result Truncation**: `PruneToolResult` truncates tool outputs at 4,000 bytes / 80 lines. Reading standard 100-line source files gets sliced in the middle with `[... truncated 30 lines ...]`, causing coding agents to fail edits or hallucinate code.
6. **Frozen Streaming Experience with Web Accounts**: `wrapWebStream` buffers the entire text stream and only parses `<thought>` tags after connection close, leaving developers staring at a frozen terminal for 30-60 seconds during complex reasoning turns.

Fixing these flaws transforms AMUX from a promising but frustrating utility into a rock-solid, transparent, and delightful AI gateway for modern IDE agents.

## What Changes

- **Daemon Logging & Observability**:
  - In `pkg/gateway/gateway.go`, route detached daemon stdout/stderr directly into `~/.amux/gateway.log`.
  - In `pkg/telemetry/logger.go`, ensure all logging targets `~/.amux/gateway.log` using `types.BaseDir()`.
  - In `pkg/proxy/server.go` and `pkg/gateway/gateway.go`, initialize file logging on startup.
- **Zero-Pollution Sandbox & Provider Pinning in `amux run`**:
  - Remove silent persistent file mutation in `amux run` unless `--setup` is explicitly supplied.
  - Add `--provider` / `-p` flag support to `amux run <ide> -p <provider> [args...]`.
  - Support path-based (`/p/<provider>/...`, `/provider/<provider>/...`) and query-based (`?provider=<provider>`) provider routing in `pkg/proxy/server.go`.
- **Web Tool Semantic Descriptions**:
  - In `pkg/tools/webloop.go` `catalogLine`, append concise tool descriptions when available (`Name:args — Description`).
- **Realistic Tool Output Pruning**:
  - Increase pruning thresholds in `pkg/tools/prune.go` from 4,000 bytes / 80 lines to 24,000 bytes / 300 lines, with generous head/tail retention.
- **Live Thinking Streaming**:
  - In `pkg/tools/webloop.go` `wrapWebStream`, extract and stream `<thought>` / `<thinking>` content in real-time as chunks arrive rather than delaying until stream termination.

## Capabilities

### Modified Capabilities
- `gateway-http`: Support path-based and query-based provider selection (`/p/<provider>/...`, `?provider=...`), ensure full logging to `~/.amux/gateway.log`.
- `ide-integration`: Strictly enforce zero-pollution sandbox unless `--setup` is passed; add `-p` / `--provider` flag support to `amux run`.
- `web-tool-optimization`: Include tool descriptions in WebPreamble catalog, increase code output pruning thresholds to 24KB / 300 lines, and enable real-time thinking delta streaming.

## Impact

- `pkg/gateway/gateway.go`
- `pkg/telemetry/logger.go`
- `pkg/cli/run.go`
- `pkg/proxy/server.go`
- `pkg/tools/webloop.go`
- `pkg/tools/prune.go`
- Relevant tests in `pkg/gateway/`, `pkg/cli/`, `pkg/proxy/`, `pkg/tools/`
