## ADDED Requirements

### Requirement: Switch without re-login
`amux switch <id>` SHALL make a saved subscription login the active login of its tool without a browser login, saving the outgoing login first. It MUST NOT write another account's credentials into a saved login, and for Claude Code it MUST replace only the account keys (`oauthAccount`) in `~/.claude.json`. Logging in with `amux login <tool>` SHALL save the previous login first and write the credential where the tool reads it.

#### Scenario: Switch back and forth
- **WHEN** a user switches from account A to B and later back to A
- **THEN** both switches succeed without logging in, including while the gateway runs

#### Scenario: Claude config preserved
- **WHEN** an MCP server was added to `~/.claude.json` after account B was saved
- **THEN** switching to B keeps that MCP server
