# anti-ban-guard Specification

## Purpose
Protect user accounts from abuse detection and runaway retries.

## Requirements

### Requirement: Pacing and circuit breaking
Every upstream call SHALL pass through per-account pacing (with jitter for web accounts); repeated failures MUST quarantine the account for a back-off period.

#### Scenario: Repeated 403s
- **WHEN** an account fails several times in a row
- **THEN** it is quarantined and skipped until the quarantine expires

### Requirement: Header sanitizing and egress isolation
Outgoing requests SHALL strip client-identifying headers that do not belong to the upstream, and MAY use a per-account egress proxy (`proxy` field: http, https or socks5).

#### Scenario: Per-account proxy
- **WHEN** an account has `proxy: socks5://…`
- **THEN** all its upstream traffic goes through that proxy
