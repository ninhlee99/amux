## ADDED Requirements

### Requirement: Web sessions on paid plans stay web identities
Migration SHALL classify a provider whose type is a web type as tier `web` with `cdp` auth regardless of its plan. Loading identities SHALL repair web identities (id containing `:web:`, `chatgpt:` ids, or a "… Web" provider) stored as subscriptions back to active web identities.

#### Scenario: Gemini Web on Google AI Pro
- **WHEN** accounts.json lists `gemini:web:x` of type `gemini_web` with plan `pro`
- **THEN** the identity is tier `web`, auth `cdp`, active
