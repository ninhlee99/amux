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

### Requirement: Unified account login and health check
The CLI SHALL provide a streamlined `amux login [brand]` command supporting OAuth PKCE, Web session cookie import, and API keys through a single entrypoint with immediate connection health verification.

#### Scenario: Unified login with validation
- **WHEN** a user completes login for any provider
- **THEN** amux verifies upstream connection latency and health before adding the account to storage

### Requirement: One account per product per email
amux SHALL keep at most one account row per product and email, where products are ChatGPT Web, Codex, Claude Web, Claude Code, Antigravity, Gemini Web and Gemini API. Accounts of different products with the same email MUST stay separate rows, and a login MUST NOT take over or overwrite another product's row.

#### Scenario: ChatGPT Web login with a Codex subscription on the same email
- **WHEN** a user with a Codex subscription for `me@x.com` runs `amux login chatgpt` signed in as `me@x.com`
- **THEN** a ChatGPT Web row for `me@x.com` is saved under a `chatgpt:` id and the Codex row is unchanged

#### Scenario: Relogin of an account saved without email
- **WHEN** a ChatGPT Web row was saved without an email and the same account logs in again
- **THEN** that row learns its email and is updated in place; no second row is added

### Requirement: Active browser session is used
When auto-extracting a web session cookie, amux SHALL choose the most recently used unexpired session across all supported browsers and profiles.

#### Scenario: Two profiles signed in
- **WHEN** Edge "Default" and "Profile 2" are both signed in to chatgpt.com and Profile 2 was used last
- **THEN** the Profile 2 session is imported

### Requirement: Accounts listed by provider and email
`amux accounts` and the table printed after a login SHALL show the provider name and account email rather than internal ids, and account commands SHALL accept an email, `provider:email`, or the row number.

#### Scenario: Email on two providers
- **WHEN** a user runs `amux switch me@x.com` and that email is on Codex and ChatGPT Web
- **THEN** amux names both choices (`codex:me@x.com`, `chatgpt:me@x.com`) instead of picking one silently

### Requirement: Credential expiration and health diagnostics
The CLI command `amux doctor auth` SHALL inspect all configured identities and accounts, extracting expiration times from OAuth JWTs or session metadata, and display their status: healthy, expiring soon, expired, or missing credentials.

#### Scenario: Active OAuth token inspected
- **WHEN** `amux doctor auth` is invoked on an identity with a valid unexpired JWT
- **THEN** the output reports the credential as healthy and indicates the remaining expiration period

#### Scenario: Expired token inspected
- **WHEN** an identity's JWT expiration time has passed
- **THEN** `amux doctor auth` reports the credential as expired and suggests re-authenticating with `amux login <provider>`


