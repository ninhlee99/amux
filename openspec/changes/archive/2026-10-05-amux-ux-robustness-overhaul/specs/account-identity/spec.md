## MODIFIED Requirements

### Requirement: Single storage root
All state SHALL live under `~/.amux` (or `$AMUX_HOME` / `$AM_HOME` / `$AM_DIR`): `identities.json`, `accounts.json`, `config.json`, profiles, logs, browser profiles. Legacy `~/.am` data MUST be migrated non-destructively on first run.

#### Scenario: First run after upgrade
- **WHEN** `~/.amux` does not exist but `~/.am` does
- **THEN** amux copies `~/.am` into `~/.amux` without deleting the original

## ADDED Requirements

### Requirement: Unified account login and health check
The CLI SHALL provide a streamlined `amux login [brand]` command supporting OAuth PKCE, Web session cookie import, and API keys through a single entrypoint with immediate connection health verification.

#### Scenario: Unified login with validation
- **WHEN** a user completes login for any provider
- **THEN** amux verifies upstream connection latency and health before adding the account to storage
