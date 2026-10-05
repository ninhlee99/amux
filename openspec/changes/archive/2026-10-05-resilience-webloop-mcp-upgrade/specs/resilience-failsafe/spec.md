## ADDED Requirements

### Requirement: Transparent direct fallback on pool exhaustion
When a hooked IDE client makes a request to `/v1/messages` and the pool router cannot serve the request (due to all pool accounts being in cooldown, exhausted, or unavailable), the gateway SHALL check for valid native subscription credentials in the OS keychain/environment and proxy the request directly upstream instead of terminating with a network error.

#### Scenario: Fallback to native Anthropic API on pool exhaustion
- **WHEN** all pool adapters return rate limit or cooldown errors for an incoming `/v1/messages` request and Claude Code credentials exist in keychain
- **THEN** the gateway transparently forwards the request upstream to `api.anthropic.com` using the native credentials and streams the response to the client
