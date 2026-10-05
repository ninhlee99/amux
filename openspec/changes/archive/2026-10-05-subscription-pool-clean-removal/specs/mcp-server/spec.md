## MODIFIED Requirements

### Requirement: Pool tools
The server SHALL expose `amux_providers` (accounts without secrets), `amux_status` (gateway URLs per client dialect and usable account counts) and `amux_ask` (send a self-contained prompt through the account pool). `amux_ask.provider` SHALL accept an exact account id (pins it) or an account family such as `gemini:web` (amux picks within it with failover). Automatic selection in `amux_ask` MUST exclude subscription accounts unless pinned or added to the pool with `amux pool add`.

#### Scenario: Ask with automatic selection
- **WHEN** `amux_ask` is called without `provider` and the pool has a Claude subscription (not added to the pool) and a Gemini web account
- **THEN** the Gemini web account answers and the result names it in `provider`

#### Scenario: Ask a family
- **WHEN** `amux_ask` is called with `provider: "chatgpt"` and two ChatGPT web accounts exist
- **THEN** one of them answers, chosen by amux, and the result names it

## ADDED Requirements

### Requirement: Clean MCP removal
`amux mcp uninstall` with no host SHALL remove the amux entry from every host where it is registered (detected or not), delete the install-time `.amux.bak`, and delete the config file when amux created it and it is empty afterwards.

#### Scenario: Config amux created
- **WHEN** `amux mcp install cursor` created `~/.cursor/mcp.json` and the user runs `amux mcp uninstall`
- **THEN** the file no longer exists
