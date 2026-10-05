## MODIFIED Requirements

### Requirement: Manual-only accounts are never auto-selected
Accounts with `AUTO-SWITCH: OFF` (manual only) or disabled (`amux account off`) MUST NOT be chosen by automatic rotation, failover or account-family selection in any tier; an explicit exact-id pin of a manual-only (not disabled) account SHALL still reach it.

#### Scenario: Manual-only account
- **WHEN** the only healthy account is marked manual-only
- **THEN** automatic routing skips it and reports that no account is available

#### Scenario: Explicit pin of a manual-only account
- **WHEN** a request pins `groq:api:01`, which is manual-only
- **THEN** that account serves the request
