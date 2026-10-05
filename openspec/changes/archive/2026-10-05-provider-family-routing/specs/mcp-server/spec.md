## MODIFIED Requirements

### Requirement: Pool tools
The server SHALL expose `amux_providers` (accounts without secrets), `amux_status` (gateway URLs per client dialect and usable account counts) and `amux_ask` (send a self-contained prompt through the account pool). `amux_ask.provider` SHALL accept an exact account id (pins it) or an account family such as `gemini:web` (amux picks within it with failover). Automatic selection in `amux_ask` MUST exclude subscription accounts unless pinned or `AMUX_MCP_ALLOW_SUBSCRIPTION=1`.

#### Scenario: Ask with automatic selection
- **WHEN** `amux_ask` is called without `provider` and the pool has a Claude subscription and a Gemini web account
- **THEN** the Gemini web account answers and the result names it in `provider`

#### Scenario: Ask a family
- **WHEN** `amux_ask` is called with `provider: "chatgpt"` and two ChatGPT web accounts exist
- **THEN** one of them answers, chosen by amux, and the result names it
