# ide-integration Specification

## Purpose
amux wires coding agents to the gateway without wrapping how they are launched: it edits each tool's own configuration and environment, and undoes the edit exactly.
## Requirements
### Requirement: Hook and unhook per IDE
`amux hook` SHALL point Claude Code (`~/.claude/settings.json` env), Codex (`~/.codex/config.toml`), Cursor settings and Antigravity (settings, shell rc block, launchctl) at the gateway, and `amux unhook` MUST remove only values amux wrote. Settings amux fills in only when unset (Antigravity `modelProvider`, `GEMINI_API_KEY`, Claude `ANTHROPIC_AUTH_TOKEN=am-proxy`) MUST be removed only if amux set them. Unhook MUST NOT create files, MUST follow symlinked rc files and keep their mode, and SHALL delete config files that amux created and that are empty afterwards. Hooks MUST change only on an explicit command; nothing hooks or unhooks in the background. Hooking Codex MUST NOT set a session-wide `OPENAI_BASE_URL`.

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
- **THEN** after `amux mcp install <host>` it can call `amux_ask` and `muse_chat`

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

