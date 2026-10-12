## ADDED Requirements

### Requirement: Failed pools never fall back to an unpooled subscription
When every pool account fails, the gateway SHALL fall back to the calling tool's own Claude Code / Codex login only if a subscription of that product is enabled and in the rotation pool, and never for a request pinned to a specific account.

#### Scenario: pinned web account fails, Claude Code is off
- **WHEN** a request pinned to `chatgpt:x` fails and the Claude Code subscription is not in the pool
- **THEN** the client receives the error and the Claude Code login is not used
