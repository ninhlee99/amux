## MODIFIED Requirements

### Requirement: Hook and unhook per IDE
`amux hook` SHALL point Claude Code (`~/.claude/settings.json` env), Codex (`~/.codex/config.toml`), Cursor settings and Antigravity (shell rc / env) at the gateway, and `amux unhook` MUST remove only values amux wrote (gateway URLs on port 8787, amux placeholder credentials). When hooking Claude Code, amux SHALL also set `ANTHROPIC_AUTH_TOKEN=am-proxy` if no token is configured, so Claude Code needs no keychain login of its own behind the gateway; unhook MUST remove that placeholder.

#### Scenario: Unhook preserves user values
- **WHEN** `ANTHROPIC_BASE_URL` points at a non-amux host
- **THEN** `amux unhook --claude` leaves it untouched

#### Scenario: Placeholder token
- **WHEN** `amux hook --claude` runs on a settings file without `ANTHROPIC_AUTH_TOKEN`
- **THEN** the env gets `ANTHROPIC_AUTH_TOKEN=am-proxy`, and `amux unhook --claude` removes it again

## ADDED Requirements

### Requirement: MCP as a second integration path
Besides env/config hooks, amux SHALL offer `amux mcp install` so any MCP-capable agent reaches the user's chat platforms as tools, independent of which model the agent itself runs on.

#### Scenario: Agent without base-URL support
- **WHEN** an agent cannot point its model traffic at the gateway but supports MCP
- **THEN** after `amux mcp install <host>` it can call `amux_ask` and `muse_chat`
