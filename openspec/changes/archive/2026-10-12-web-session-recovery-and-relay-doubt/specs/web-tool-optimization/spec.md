## ADDED Requirements

### Requirement: Relay doubt is retried on a fresh thread, not argued with
A web reply without a tool call that declines because the model doubts the relayed tool results SHALL reset the web thread and be retried once with the unchanged request. If the retry declines again, the turn SHALL fail with an error before any content so the router can try another account.

#### Scenario: claude.ai doubts the relay once
- **WHEN** Claude Web replies that it cannot verify the relayed [Tool result]s and the retry on a fresh chat continues the task
- **THEN** the client receives the retry's reply only
