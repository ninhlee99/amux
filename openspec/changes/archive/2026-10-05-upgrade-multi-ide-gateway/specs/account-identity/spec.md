## MODIFIED Requirements

### Requirement: Tests never touch real user state
Code running inside a Go test binary MUST NOT read or write the developer's real `~/.amux`, OS keychain, or Claude Code credential file when no override is set.

#### Scenario: Test without AMUX_HOME
- **WHEN** a test calls `types.BaseDir()` with no override
- **THEN** it gets a per-process temporary directory

#### Scenario: Test storing a secret
- **WHEN** a test calls `auth.KCSet` without `AMUX_SECRET_STORE`
- **THEN** the secret lands in the temporary vault and the keychain is untouched
