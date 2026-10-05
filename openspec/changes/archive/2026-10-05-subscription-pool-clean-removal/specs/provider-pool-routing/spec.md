## ADDED Requirements

### Requirement: Subscriptions join automatic routing only by hand
Subscription adapters (Claude Code, Codex, Antigravity plans) MUST NOT be chosen by automatic rotation, failover or family selection unless their account was added to the pool with `amux pool add`; an exact-id pin SHALL still reach them. Web and API-key accounts SHALL be in the pool unless removed.

#### Scenario: Web account limited, subscription not pooled
- **WHEN** the only web account is rate limited and a Codex subscription is not in the pool
- **THEN** routing reports that no account is available instead of using the subscription

#### Scenario: Pinned subscription
- **WHEN** a request pins `codex:01`, which is not in the pool
- **THEN** that account serves the request
