## ADDED Requirements

### Requirement: Retryable status for fast routing failures
For a streaming Anthropic Messages request the gateway SHALL open the SSE stream only once routing has taken 5 seconds (or a keepalive is due), so a routing failure within that time is answered with an HTTP error status instead of a stream that ends in an error event. An unreachable upstream or its cooldown SHALL be answered with `503` and a `Retry-After` matching the back-off (10 seconds, 30 for a Cloudflare challenge); error messages SHALL name the reason and remaining cooldown and MUST NOT contain upstream HTML.

#### Scenario: Offline Claude Code turn
- **WHEN** Claude Code sends a streaming turn while the only pool account is unreachable
- **THEN** it receives HTTP 503 with `Retry-After` and no `message_start`, and retries the turn itself
