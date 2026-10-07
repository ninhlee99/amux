## ADDED Requirements

### Requirement: Credential expiration and health diagnostics
The CLI command `amux doctor auth` SHALL inspect all configured identities and accounts, extracting expiration times from OAuth JWTs or session metadata, and display their status: healthy, expiring soon, expired, or missing credentials.

#### Scenario: Active OAuth token inspected
- **WHEN** `amux doctor auth` is invoked on an identity with a valid unexpired JWT
- **THEN** the output reports the credential as healthy and indicates the remaining expiration period

#### Scenario: Expired token inspected
- **WHEN** an identity's JWT expiration time has passed
- **THEN** `amux doctor auth` reports the credential as expired and suggests re-authenticating with `amux login <provider>`
