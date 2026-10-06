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
The server SHALL expose `amux_providers` (accounts without secrets), `amux_status` (gateway URLs per client dialect and usable account counts), `amux_ask` (send a self-contained prompt through the account pool with optional workspace context), `amux_review` (multi-file diff code analysis), `amux_diagnose` (error and stack trace investigation), `amux_fix` (code patch and bug fix generation), and `amux_analyze` (project architecture and design analysis). `amux_ask.provider` SHALL accept an exact account id (pins it) or an account family such as `gemini:web` (amux picks within it with failover). Automatic selection in pool tools MUST exclude subscription accounts unless pinned or added to the pool with `amux pool add`. Results returned by pool tools SHALL sanitize raw web markup, stripping internal `<thought>` blocks and `[tool_call]` markers before returning output to the client.

#### Scenario: Sanitize raw web tool markup in tool output
- **WHEN** a web backend answers `amux_ask` or `amux_review` with thinking tags `<thought>...</thought>` or emulated `[tool_call]` blocks
- **THEN** the returned text is stripped of internal web markup so the MCP client receives clean assistant content

#### Scenario: Ask with automatic selection
- **WHEN** `amux_ask` is called without `provider` and the pool has a Claude subscription (not added to the pool) and a Gemini web account
- **THEN** the Gemini web account answers and the result names it in `provider`

#### Scenario: Ask a family
- **WHEN** `amux_ask` is called with `provider: "chatgpt"` and two ChatGPT web accounts exist
- **THEN** one of them answers, chosen by amux, and the result names it

#### Scenario: Review code diff
- **WHEN** `amux_review` is called with `diff` and `focus`
- **THEN** a senior code review is routed through the pool and returned to the client

#### Scenario: Diagnose error
- **WHEN** `amux_diagnose` is called with `error` and relevant code
- **THEN** an expert root cause analysis and resolution steps are returned

#### Scenario: Fix bug
- **WHEN** `amux_fix` is called with `file_content` and `issue`
- **THEN** a corrected code patch is returned

#### Scenario: Analyze architecture
- **WHEN** `amux_analyze` is called with `structure` and `objective`
- **THEN** architectural recommendations are returned

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

### Requirement: Resilient Web Account MCP tool dispatch
MCP tools (`amux_ask`, `amux_review`, `amux_diagnose`, `amux_fix`, `amux_analyze`) SHALL automatically sanitize internal web markup and handle transient web auth/rate limits with graceful retries or actionable diagnostic messages.

#### Scenario: Clean MCP tool output from web providers
- **WHEN** an MCP tool request is routed to a web account that outputs `<thought>` or `[tool_call]` markup
- **THEN** the returned output contains only clean markdown analysis without raw delimiters

### Requirement: Expanded MCP Host configuration
`amux mcp install` SHALL support registration into Cline, Roo Code, Windsurf, Zed, and VS Code with standard paths and automatic profile backups.

#### Scenario: Installing MCP in Cline and Roo Code
- **WHEN** user executes `amux mcp install roo` or `amux mcp install cline`
- **THEN** AMUX configures the `cline_mcp_settings.json` file with the amux stdio server entry

