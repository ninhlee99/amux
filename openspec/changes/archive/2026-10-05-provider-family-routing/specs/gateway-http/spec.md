## MODIFIED Requirements

### Requirement: Pinning a provider per request
The gateway SHALL accept `X-Provider` with either an exact pool account id or an account family. An exact id (e.g. `gemini:web:01`) MUST route to that account only — even when it is out of the automatic rotation — with no failover, and MUST be refused for accounts the user disabled. A family (`gemini:web`, `gemini:web:*`, `chatgpt`; case-insensitive, matched on whole `:`-separated segments) SHALL let amux choose among that family's accounts with the pool's affinity, cooldown, quarantine and failover rules, never leaving the family.

#### Scenario: Explicit pin
- **WHEN** a request has `X-Provider: gemini:web:01`
- **THEN** only `gemini:web:01` serves it and no failover to another account happens

#### Scenario: Family pick with failover
- **WHEN** a request has `X-Provider: gemini:web`, `gemini:web:01` is rate-limited and `gemini:web:02` is healthy
- **THEN** `gemini:web:02` serves it and no account outside `gemini:web` is tried

#### Scenario: Unknown family
- **WHEN** a request has `X-Provider: gemini:w`
- **THEN** the request fails with "matches no account" instead of routing elsewhere
