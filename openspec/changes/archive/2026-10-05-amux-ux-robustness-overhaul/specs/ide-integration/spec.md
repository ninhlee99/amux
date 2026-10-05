## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Isolated runtime runner
The CLI SHALL provide `amux run <command>` which launches any tool/IDE with temporary injected environment variables in a child process without mutating global shell profile files or launchctl.

#### Scenario: Running client in sandbox wrapper
- **WHEN** user executes `amux run claude` or `amux run cursor`
- **THEN** the child process inherits the proxy environment while global system configuration remains untouched
