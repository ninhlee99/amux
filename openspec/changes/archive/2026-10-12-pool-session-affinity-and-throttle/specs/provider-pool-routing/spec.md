## ADDED Requirements

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
