# provider-pool-routing Specification

## Purpose
The `AccountPoolRouter` decides which account serves each request across three tiers — subscriptions (OAuth), web sessions, and metered API keys — with failover, cooldowns, session affinity and task-aware preferences.
## Requirements
### Requirement: Tiered failover
The router SHALL try accounts in priority order within the allowed tiers and fail over to the next account when one returns an authentication, rate-limit or upstream error before producing output.

#### Scenario: Rate-limited account
- **WHEN** the first account answers 429 with `Retry-After: 60`
- **THEN** the router cools that account down for about 60s and serves the request from the next account

### Requirement: Manual-only accounts are never auto-selected
Accounts outside the rotation pool (`amux pool remove`, and subscriptions never added with `amux pool add`) or disabled (`amux account off`) MUST NOT be chosen by automatic rotation, failover or account-family selection in any tier; an explicit exact-id pin of a manual-only (not disabled) account SHALL still reach it.

#### Scenario: Manual-only account
- **WHEN** the only healthy account is marked manual-only
- **THEN** automatic routing skips it and reports that no account is available

#### Scenario: Explicit pin of a manual-only account
- **WHEN** a request pins `groq:api:01`, which is manual-only
- **THEN** that account serves the request

### Requirement: Native-tool backends first
When a request carries client `tools[]` and an account with native function calling is available, the router SHALL skip text-only web/Codex backends (`skipTextOnly`).

#### Scenario: Tools present
- **WHEN** Claude Code sends tools and a Gemini API key is healthy
- **THEN** the Gemini API account is used before any web session

### Requirement: Claude subscription is not a proxy backend for Claude clients
When the client is Claude Code, the `claude_sub` group MUST NOT be used as a pool backend behind the gateway.

#### Scenario: Claude Code through the gateway
- **WHEN** Claude Code's request is routed by the pool
- **THEN** no `claude_sub` account is selected for it

### Requirement: Session affinity
Requests from the same client session SHALL stick to the account that served the session's first request while that account stays healthy, so web threads and prompt caches are reused.

#### Scenario: Second turn of a session
- **WHEN** turn 2 of a session arrives and its pinned account is healthy
- **THEN** the same account serves it

### Requirement: Privacy redaction before send
Outbound payloads SHALL pass through redaction of secrets (API keys, tokens) before reaching any upstream.

#### Scenario: Secret in prompt
- **WHEN** a message contains an `sk-…` key
- **THEN** the upstream receives a redacted placeholder

### Requirement: Subscriptions join automatic routing only by hand
Subscription adapters (Claude Code, Codex, Antigravity plans) MUST NOT be chosen by automatic rotation, failover or family selection unless their account was added to the pool with `amux pool add`; an exact-id pin SHALL still reach them. Web and API-key accounts SHALL be in the pool unless removed.

#### Scenario: Web account limited, subscription not pooled
- **WHEN** the only web account is rate limited and a Codex subscription is not in the pool
- **THEN** routing reports that no account is available instead of using the subscription

#### Scenario: Pinned subscription
- **WHEN** a request pins `codex:01`, which is not in the pool
- **THEN** that account serves the request

### Requirement: Healthy accounts first within a tier
Within one tier, the router SHALL try accounts whose health score is below 80 (degraded) only after the healthy accounts of that tier, keeping round-robin order among accounts of equal health. Quarantined and cooling accounts remain skipped.

#### Scenario: Flaky web account
- **WHEN** two web accounts are in the pool and one has dropped to health 70 after 5xx errors
- **THEN** new turns are served by the healthy account while it keeps succeeding

