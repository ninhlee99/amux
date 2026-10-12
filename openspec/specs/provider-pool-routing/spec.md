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

### Requirement: In-stream errors before output fail over
The router SHALL wait for a stream's first content, thinking or tool call before committing to an account. An error chunk that arrives before any of these SHALL be treated like a synchronous send error: the account's error is recorded, rate limits cool it down, and the next eligible account is tried. Errors after output has started SHALL be passed to the client unchanged.

#### Scenario: web account reports its usage limit inside the stream
- **WHEN** account A returns a stream whose first chunk is a rate-limit error and account B is eligible
- **THEN** the client receives B's answer, A is cooling down, and later requests skip A

### Requirement: Cooldown follows the upstream reset time
When a rate-limit error carries a reset hint, the router SHALL cool the account down until then (clamped to 5 s – 30 min) instead of the fixed default.

#### Scenario: claude.ai limit resets in 90 minutes
- **WHEN** Claude Web returns a limit body with `resetsAt` 90 minutes ahead
- **THEN** the error's RetryAfter is about 90 minutes and the account is cooled for the clamped maximum

### Requirement: Automatic switches never pin
When the gateway falls back from Claude to the provider pool, it SHALL record the serving account for status only; it SHALL NOT create a manual pin, so new sessions keep load-balancing across the pool.

#### Scenario: Claude Code is off
- **WHEN** eight new sessions arrive while Claude Code is turned off
- **THEN** they are spread over the pooled web accounts instead of all going to the first one used

### Requirement: The router owns session affinity
Bridges SHALL NOT pin sessions before routing unless a manual pin exists; a session SHALL stay on the account that first served it while that account is usable, and its history SHALL be compacted only when it moves to another account.

#### Scenario: multi-turn session
- **WHEN** one session sends three turns and its account stays healthy
- **THEN** all three turns are served by the same account with full history

### Requirement: Concurrent new sessions spread immediately
The router SHALL advance its round-robin position when it picks an account, not when the stream succeeds, so sessions that start concurrently are spread over the pool before any of them has received output.

#### Scenario: four Claude Code sessions start at once
- **WHEN** four new sessions arrive within a second on a pool of four web accounts
- **THEN** each account receives one session

