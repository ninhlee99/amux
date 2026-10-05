## MODIFIED Requirements

### Requirement: MCP as a second integration path
Besides env/config hooks, amux SHALL offer `amux mcp install` so any MCP-capable agent reaches the user's chat platforms as tools, independent of which model the agent itself runs on.

#### Scenario: Agent without base-URL support
- **WHEN** an agent cannot point its model traffic at the gateway but supports MCP
- **THEN** after `amux mcp install <host>` it can call `amux_ask` and inspect providers via `amux_providers`
