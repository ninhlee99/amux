## MODIFIED Requirements

### Requirement: Pacing and circuit breaking
Every upstream call SHALL pass through per-account pacing (with jitter for web accounts); repeated failures MUST quarantine the account for a back-off period. An unreachable upstream — no network (DNS, no route, refused or reset connection) or a Cloudflare challenge page instead of the API — MUST NOT count as an account failure: it SHALL NOT lower the health score, count toward the auth-failure streak or quarantine the account, and the router SHALL cool the account down only briefly (10 seconds, or 30 seconds for a Cloudflare challenge). A 401/403 that is not a Cloudflare challenge stays an auth failure.

#### Scenario: Repeated 403s
- **WHEN** an account fails several times in a row
- **THEN** it is quarantined and skipped until the quarantine expires

#### Scenario: Network drops
- **WHEN** the network drops and chatgpt.com answers every request with a Cloudflare challenge page
- **THEN** the ChatGPT account is never quarantined and is retried 30 seconds after each challenge (10 seconds after a dead-network failure), serving again as soon as chatgpt.com does
