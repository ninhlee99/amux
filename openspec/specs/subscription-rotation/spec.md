# subscription-rotation Specification

## Purpose
Use several subscription accounts of the same product (Claude, Codex, Antigravity) one after another as quotas run out.
## Requirements
### Requirement: Threshold-based rotation
When the active subscription is in the rotation pool and its 5h/7d usage reaches its threshold (default 95%, per account via `amux account threshold`), and another pooled subscription of the same product is usable, amux SHALL switch the active account to it. Subscriptions MUST NOT be in the pool unless the user added them with `amux pool add`; amux MUST NOT switch to, or away from, an account outside the pool automatically.

#### Scenario: Quota at 96% with both accounts pooled
- **WHEN** pooled Sub-1 reports 96% of its 5h window and pooled Sub-2 is at 10%
- **THEN** Sub-2 becomes active for new requests

#### Scenario: Other account not in the pool
- **WHEN** Sub-1 hits its limit and Sub-2 was never added to the pool
- **THEN** Sub-1 stays active and Sub-2 is not used

#### Scenario: Login does not pool
- **WHEN** the user logs in a second Claude Code account
- **THEN** neither account is in the pool until `amux pool add`

