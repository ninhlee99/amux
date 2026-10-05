# mcp-server Specification

## Purpose
Let any MCP-capable coding agent (Claude Code, Claude Desktop, Cursor, Codex, Gemini CLI, Antigravity, VS Code, Windsurf, opencode, Zed) use the chat platforms amux manages as tools, and register amux in those hosts safely.
## Requirements
### Requirement: Stdio MCP server
`amux mcp` SHALL run a Model Context Protocol server over newline-delimited JSON-RPC 2.0 on stdin/stdout, negotiating protocol versions `2025-06-18`, `2025-03-26` and `2024-11-05` (falling back to the newest for unknown versions), and MUST write nothing but protocol messages to stdout.

#### Scenario: Version negotiation
- **WHEN** a client initializes with `protocolVersion: "2025-03-26"`
- **THEN** the server answers with `2025-03-26` and advertises the `tools` capability

#### Scenario: Stray output
- **WHEN** a library prints to stdout during a tool call
- **THEN** the text goes to stderr and the JSON-RPC stream stays valid

### Requirement: Concurrent, cancellable tool calls
The server SHALL process requests concurrently, cancel a running call on `notifications/cancelled`, send throttled `notifications/progress` when the call carries `_meta.progressToken`, and report tool failures as results with `isError: true`.

#### Scenario: Ping during a long call
- **WHEN** `amux_ask` is still running and the client sends `ping`
- **THEN** the ping is answered immediately

#### Scenario: Tool failure
- **WHEN** `amux_ask` names an unknown provider
- **THEN** the response is a successful JSON-RPC result with `isError: true` and the reason as text

### Requirement: Pool tools
The server SHALL expose `amux_providers` (accounts without secrets), `amux_status` (gateway URLs per client dialect and usable account counts) and `amux_ask` (send a self-contained prompt through the account pool). `amux_ask.provider` SHALL accept an exact account id (pins it) or an account family such as `gemini:web` (amux picks within it with failover). Automatic selection in `amux_ask` MUST exclude subscription accounts unless pinned or added to the pool with `amux pool add`.

#### Scenario: Ask with automatic selection
- **WHEN** `amux_ask` is called without `provider` and the pool has a Claude subscription (not added to the pool) and a Gemini web account
- **THEN** the Gemini web account answers and the result names it in `provider`

#### Scenario: Ask a family
- **WHEN** `amux_ask` is called with `provider: "chatgpt"` and two ChatGPT web accounts exist
- **THEN** one of them answers, chosen by amux, and the result names it

### Requirement: Muse tools
The server SHALL expose `muse_status`, `muse_login`, `muse_new_chat`, `muse_chat`, `muse_read_last`, `muse_chats`, `muse_open_chat`, `muse_read_chat`, `muse_media`, `muse_dump_dom` and `muse_close`, starting the Muse browser lazily on first use.

#### Scenario: Chat with an attachment
- **WHEN** `muse_chat` is called with `prompt` and `files: ["/tmp/a.png"]`
- **THEN** the image is attached in the composer and the full reply plus thread URL is returned

### Requirement: Host registration
`amux mcp install [host…|all]` SHALL register `<amux binary> mcp` in Claude Code (via `claude mcp add --scope user` when available, else `~/.claude.json`), Claude Desktop, Cursor, Windsurf, VS Code (`servers` key), Gemini CLI, Antigravity, Codex (`[mcp_servers.amux]` in `config.toml`), opencode (`mcp` key, local command array) and Zed (`context_servers`). Edits MUST be atomic, idempotent, keep a one-time `.amux.bak`, preserve other entries and file permissions, and MUST NOT rewrite files containing comments (the snippet is printed instead). `uninstall` removes only the amux entry.

#### Scenario: Install into Cursor with other servers
- **WHEN** `~/.cursor/mcp.json` already lists another server
- **THEN** after install both servers are present and a backup of the original exists

#### Scenario: JSONC settings
- **WHEN** Zed's `settings.json` contains `//` comments
- **THEN** amux leaves the file unchanged and prints the `context_servers` snippet

### Requirement: Clean MCP removal
`amux mcp uninstall` with no host SHALL remove the amux entry from every host where it is registered (detected or not), delete the install-time `.amux.bak`, and delete the config file when amux created it and it is empty afterwards.

#### Scenario: Config amux created
- **WHEN** `amux mcp install cursor` created `~/.cursor/mcp.json` and the user runs `amux mcp uninstall`
- **THEN** the file no longer exists

