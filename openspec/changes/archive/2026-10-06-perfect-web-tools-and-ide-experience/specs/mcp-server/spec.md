## ADDED Requirements

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
