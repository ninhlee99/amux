## ADDED Requirements

### Requirement: Gemini throttling rests the account
When a Gemini Web request stalls and its retry on a fresh chat stalls as well, the adapter SHALL fail with a rate-limit error carrying a 5-minute RetryAfter.

#### Scenario: latency keeps climbing
- **WHEN** two consecutive Gemini attempts make no progress in time
- **THEN** the router cools the account down and serves the turn from another account

### Requirement: ChatGPT citation markers are removed
ChatGPT Web text SHALL NOT contain its private-use citation markers (U+E200 … U+E201).

#### Scenario: web search answer
- **WHEN** ChatGPT answers with `citeturn0search0`
- **THEN** the client receives the answer without the marker
