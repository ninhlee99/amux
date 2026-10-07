# Technical Design: Flawless IDE and WebLoop Experience

## Architecture & Data Flow

### 1. Daemon Logging Architecture

```
[amux start]
       │
       ▼
exec.Command(self, "gateway", "_daemon")
       │
       ├─ Stdout ───► ~/.amux/gateway.log (O_CREATE|O_APPEND|O_WRONLY)
       └─ Stderr ───► ~/.amux/gateway.log
       │
[RunDaemon / RunProxy]
       ▼
log.SetOutput(io.MultiWriter(os.Stderr, gatewayLogFile))
```

- When `gateway.Start()` launches the background process, it creates/appends to `~/.amux/gateway.log` with `0o644` permissions and sets `cmd.Stdout = f` and `cmd.Stderr = f`.
- In `pkg/telemetry/logger.go`, legacy `.am` is replaced with `types.BaseDir()`.
- On daemon start, timestamped startup banners are logged for complete auditability.

### 2. URL Path & Query Provider Routing

```
IDE Client Request
(e.g., ANTHROPIC_BASE_URL=http://127.0.0.1:8787/p/gemini-web)
       │
       ▼
[pkg/proxy/server.go HTTP Handler]
       │
       ├─ Check header: X-Provider
       ├─ Check URL query: ?provider=<id>
       └─ Check URL prefix: /p/<id>/... or /provider/<id>/...
               │
               ▼
       xProvider = extractedID
       strip prefix from path (normalized to /v1/messages, /v1/chat/completions)
               │
               ▼
       AccountPoolRouter.SendNamed or SendProvider
```

- If `r.URL.Path` starts with `/p/{provider}` or `/provider/{provider}`, extract `{provider}`, rewrite `r.URL.Path` to the remainder, and treat `{provider}` as explicit `xProvider`.
- If `r.URL.Query().Get("provider") != ""`, treat as explicit `xProvider`.
- This works universally with Claude Code, Cursor, Codex, Windsurf, Aider, OpenCode, and extension agents without requiring custom header support in the IDE.

### 3. Sandbox Non-Invasiveness in `amux run`

- In `CmdRun`, remove the automatic call to `hook.SyncCursorSettingsEnv(true, ...)` and `hook.SyncWindsurfSettingsEnv(true, ...)`.
- Only call setting synchronizers if `setupRequested == true` (`--setup`).
- If not `--setup`, print a non-intrusive reminder if the GUI settings are not yet set up.
- Parse `--provider` or `-p` flag in `amux run <ide> -p <provider>` and append `/p/<provider>` to `gatewayURL`.

### 4. Semantic Catalog Line in `webloop.go`

```go
func catalogLine(d types.ToolDef) string {
    keys := schemaKeyTypes(d.InputSchema, 24)
    args := strings.Join(keys, ",")
    line := d.Name
    if args != "" {
        line += ":" + args
    }
    if desc := strings.TrimSpace(d.Description); desc != "" {
        // Truncate description cleanly to 100 runes
        descRunes := []rune(desc)
        if len(descRunes) > 100 {
            desc = string(descRunes[:97]) + "..."
        }
        line += " — " + strings.ReplaceAll(desc, "\n", " ")
    }
    return line
}
```

- Gives web models (Gemini Web, ChatGPT Web, Claude Web) the intent and semantics of every tool.

### 5. Increased Pruning Limits for Realistic Code Files

```go
const (
    DefaultMaxToolOutputBytes = 24000
    DefaultMaxToolOutputLines = 300
    DefaultPruneHeadLines     = 120
    DefaultPruneTailLines     = 150
)
```

- Retains realistic file contents and test results while preventing HTTP 413 payload rejections.

### 6. Streaming Thinking Extraction

- In `wrapWebStream`, as text chunks arrive in `ch.Content`, inspect for `<thought>` / `<thinking>` openings and stream text inside `<thought>...</thought>` as `StreamChunk{Thinking: delta}` in real-time.
