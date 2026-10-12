## ADDED Requirements

### Requirement: Fresh installs keep subscriptions out of the pool
Migrating accounts.json SHALL classify CLI OAuth logins (`claude`, `claude_oauth`, `codex`, `codex_cli`, `antigravity`, `agy`) as subscriptions outside the rotation pool, keep providers with an API key as API keys, and migrate `enabled: false` as turned off. The live CLI login SHALL NOT be added as an extra "(email unknown)" subscription when that product already has one.

#### Scenario: accounts.json only
- **WHEN** amux starts with an AMUX_HOME holding only accounts.json with Claude Code (enabled:false) and Codex logins
- **THEN** both are listed as subscriptions outside the pool, Claude Code is off, and no duplicate Claude Code row appears
