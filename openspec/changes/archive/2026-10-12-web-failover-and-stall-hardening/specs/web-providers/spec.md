## ADDED Requirements

### Requirement: Gemini Web detects requests that never start
The Gemini Web adapter SHALL treat a stream as stalled when no conversation-id or text frame arrives within 45 s of sending (including the wait for response headers), or no further progress arrives for 3 minutes; heartbeat frames SHALL NOT count as progress. A stalled request SHALL be retried once in a new Gemini chat, then fail with an error.

#### Scenario: heartbeat-only stream
- **WHEN** Gemini sends only `{"37":[n]}` frames after the headers
- **THEN** the adapter abandons the stream after 45 s and retries in a new chat

### Requirement: Gemini canned failures are errors
A short reply that is one of Gemini's generic failure texts ("Sorry, something went wrong…", "I seem to be encountering an error…") SHALL be handled as an error (retry once in a new chat), and SHALL NOT be streamed to the client.

#### Scenario: canned failure
- **WHEN** Gemini answers "Sorry, something went wrong. Please try your request again."
- **THEN** nothing is streamed and the request is retried in a new chat

### Requirement: Web limit errors carry the reset time
Claude Web and ChatGPT Web rate-limit errors SHALL carry the upstream reset time (`Retry-After` header, or claude.ai `resetsAt`) as the error's RetryAfter.

#### Scenario: ChatGPT 429 with Retry-After
- **WHEN** ChatGPT answers 429 with `Retry-After: 600`
- **THEN** the returned error is a rate-limit error with RetryAfter 10 minutes

### Requirement: Refreshed sessions must belong to the configured account
When a web adapter refreshes its session from the shared browser jar, it SHALL check the refreshed session's email against the account's configured email and reject a different account (authentication error, previous cookies kept). Gemini SHALL persist browser-refreshed cookies only after this check passes.

#### Scenario: browser signed in to another ChatGPT account
- **WHEN** `chatgpt:a` (a@x.com) refreshes and the browser session is b@x.com
- **THEN** the refresh fails with an authentication error and a@x.com's slot keeps its own credentials

### Requirement: Expired ChatGPT access tokens are refreshed before the sentinel call
The ChatGPT Web adapter SHALL refresh an access token whose JWT `exp` has passed before sending, and SHALL refresh and retry once when the sentinel endpoint answers 401 / `token_expired`; a second failure SHALL be an authentication error.

#### Scenario: access token expired overnight
- **WHEN** the stored access token's exp is in the past
- **THEN** the adapter refreshes it from the session cookie before calling the sentinel endpoint
