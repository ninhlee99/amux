## ADDED Requirements

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
