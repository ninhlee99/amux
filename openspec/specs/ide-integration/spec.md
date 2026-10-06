# ide-integration Specification

## Purpose
amux wires coding agents to the gateway without wrapping how they are launched: it edits each tool's own configuration and environment, and undoes the edit exactly.
## Requirements
### Requirement: Hook and unhook per IDE
`amux hook` SHALL point Claude Code (`~/.claude/settings.json` env), Codex (`~/.codex/config.toml`), Cursor settings, Antigravity (settings, shell rc block, launchctl), Windsurf, and Cline/Roo Code at the gateway, and `amux unhook` MUST remove only values amux wrote. Settings amux fills in only when unset (Antigravity `modelProvider`, `GEMINI_API_KEY`, Claude `ANTHROPIC_AUTH_TOKEN=am-proxy`) MUST be removed only if amux set them. Unhook MUST NOT create files, MUST follow symlinked rc files and keep their mode, and SHALL delete config files that amux created and that are empty afterwards. Hooks MUST change only on an explicit command; nothing hooks or unhooks in the background. Hooking Codex MUST NOT set a session-wide `OPENAI_BASE_URL`.

#### Scenario: Unhook preserves user values
- **WHEN** `ANTHROPIC_BASE_URL` points at a non-amux host
- **THEN** `amux unhook claude` leaves it untouched

#### Scenario: Round trip
- **WHEN** a user runs `amux hook claude`, `amux hook codex`, `amux hook cursor`, then `amux unhook`
- **THEN** every config file is byte-identical to before

#### Scenario: User-owned modelProvider
- **WHEN** Antigravity settings already had `modelProvider: gemini` before `amux hook agy`
- **THEN** `amux unhook agy` keeps it

### Requirement: Shell and launchd environment
`amux env` SHALL print shell exports for the gateway and MUST NOT export `GOOGLE_API_KEY=am-proxy` globally (it breaks unrelated Google tooling).

#### Scenario: Eval env
- **WHEN** a user runs `eval "$(amux env)"`
- **THEN** Anthropic/OpenAI base URLs point at the gateway and no global Google API key placeholder is set

### Requirement: Atomic config edits
Config files SHALL be rewritten atomically (temp file + rename) preserving permissions so a crash never leaves a half-written IDE config.

#### Scenario: Interrupted write
- **WHEN** amux is killed while hooking
- **THEN** the IDE config is either the old or the new version, never truncated

### Requirement: MCP as a second integration path
Besides env/config hooks, amux SHALL offer `amux mcp install` so any MCP-capable agent reaches the user's chat platforms as tools, independent of which model the agent itself runs on.

#### Scenario: Agent without base-URL support
- **WHEN** an agent cannot point its model traffic at the gateway but supports MCP
- **THEN** after `amux mcp install <host>` it can call `amux_ask` and inspect providers via `amux_providers`

### Requirement: Back to native in one step
`amux off` SHALL unhook every tool and stop the gateway, keeping accounts and MCP registrations. `amux stop` SHALL warn about tools that are still hooked.

#### Scenario: Off
- **WHEN** Claude Code is hooked and the gateway runs
- **THEN** after `amux off` Claude Code's settings no longer reference the gateway and the gateway is stopped

### Requirement: Clean uninstall
`amux uninstall` SHALL unhook all tools, stop the gateway, remove amux session hooks and status lines, every MCP registration, `/amux` slash commands, the auto-update agent and the amux binaries (an `am` binary only if it is amux). `--purge` SHALL also delete `~/.amux` and amux's own keychain master key. Uninstall MUST NOT modify the tools' own login credentials or user-set config values.

#### Scenario: Uninstall keeps logins
- **WHEN** Claude Code is logged in and the user runs `amux uninstall --purge`
- **THEN** Claude Code is still logged in with the same account

### Requirement: Isolated runtime runner
The CLI SHALL provide `amux run <command>` which launches any tool/IDE with temporary injected environment variables in a child process without mutating global shell profile files or launchctl. For Antigravity and Gemini CLI agents, it SHALL inject `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE`, and `GOOGLE_GENAI_BASE_URL`. Before launching, `amux run` SHALL verify account availability across both `identities.json` and `accounts.json`, warning and guiding the user to login if no accounts are configured. When launching Cursor via `amux run cursor`, amux SHALL inspect Cursor's settings (`settings.json`) and inform the user whether Cursor AI Chat is already bound to `:8787/v1` or provide guidance on configuring OpenAI base URL.

#### Scenario: Running client in sandbox wrapper
- **WHEN** user executes `amux run claude` or `amux run agy`
- **THEN** the child process receives the isolated proxy endpoints while parent shell and system settings remain pristine

#### Scenario: Pre-run account availability check
- **WHEN** user executes `amux run` with neither active identities nor provider accounts configured
- **THEN** the command warns the user with actionable instructions to run `amux login`

#### Scenario: Cursor AI Chat settings inspection
- **WHEN** user executes `amux run cursor`
- **THEN** amux checks Cursor's configuration and indicates whether Cursor AI Chat is already pointed at `:8787/v1`

### Requirement: Application bundle and emerging agent detection in doctor
`amux doctor` SHALL inspect macOS `/Applications` for installed IDE bundles (Cursor, Windsurf, Zed, VS Code) in addition to PATH lookups, and SHALL report availability for emerging coding agents including `aider` and `opencode`.

#### Scenario: Windsurf and Cursor detection via Application Bundle
- **WHEN** Cursor or Windsurf is installed in `/Applications` but the CLI binary is not present in shell PATH
- **THEN** `amux doctor` detects the application and reports it as Available

### Requirement: Automated GUI IDE settings configuration
`amux run cursor --setup` and `amux run windsurf --setup` SHALL automatically configure the local IDE settings (`settings.json`) with the AMUX Gateway OpenAI base URL without requiring manual user GUI navigation.

#### Scenario: One-command Cursor setup
- **WHEN** user executes `amux run cursor --setup`
- **THEN** AMUX updates Cursor's settings with `cursor.general.openaiBaseUrl` pointing to `:8787/v1` before launching the editor

