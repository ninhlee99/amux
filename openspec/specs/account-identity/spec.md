# account-identity Specification

## Purpose
amux keeps every account the user owns — subscriptions, web sessions, API keys — in one flat store under `~/.amux`, with stable ids and encrypted secrets.
## Requirements
### Requirement: Single storage root
All state SHALL live under `~/.amux` (or `$AMUX_HOME` / `$AM_HOME` / `$AM_DIR`): `identities.json`, `accounts.json`, `config.json`, profiles, logs, browser profiles. Legacy `~/.am` data MUST be migrated non-destructively on first run.

#### Scenario: First run after upgrade
- **WHEN** `~/.amux` does not exist but `~/.am` does
- **THEN** amux copies `~/.am` into `~/.amux` without deleting the original

### Requirement: Stable account ids
Accounts SHALL use ids of the form `brand[:method]:NN` or a named suffix (e.g. `claude:web:01`, `gemini:api:02`, `chatgpt:ninhle`); re-login of the same identity MUST update the existing id in place.

#### Scenario: Re-login
- **WHEN** the same ChatGPT account logs in again
- **THEN** its existing id keeps its priority and only credentials change

### Requirement: Encrypted secrets at rest
`accounts.json` MAY be sealed as `AMENC1:` (AES-256-GCM); amux MUST transparently decrypt it and MUST keep files containing secrets at mode `0600`.

#### Scenario: Sealed accounts file
- **WHEN** `accounts.json` starts with `AMENC1:`
- **THEN** the gateway loads providers after decrypting with the master key

### Requirement: Tests never touch real user state
Code running inside a Go test binary MUST NOT read or write the developer's real `~/.amux`, OS keychain, or Claude Code credential file when no override is set.

#### Scenario: Test without AMUX_HOME
- **WHEN** a test calls `types.BaseDir()` with no override
- **THEN** it gets a per-process temporary directory

#### Scenario: Test storing a secret
- **WHEN** a test calls `auth.KCSet` without `AMUX_SECRET_STORE`
- **THEN** the secret lands in the temporary vault and the keychain is untouched

### Requirement: Switch without re-login
`amux switch <id>` SHALL make a saved subscription login the active login of its tool without a browser login, saving the outgoing login first. It MUST NOT write another account's credentials into a saved login, and for Claude Code it MUST replace only the account keys (`oauthAccount`) in `~/.claude.json`. Logging in with `amux login <tool>` SHALL save the previous login first and write the credential where the tool reads it.

#### Scenario: Switch back and forth
- **WHEN** a user switches from account A to B and later back to A
- **THEN** both switches succeed without logging in, including while the gateway runs

#### Scenario: Claude config preserved
- **WHEN** an MCP server was added to `~/.claude.json` after account B was saved
- **THEN** switching to B keeps that MCP server

