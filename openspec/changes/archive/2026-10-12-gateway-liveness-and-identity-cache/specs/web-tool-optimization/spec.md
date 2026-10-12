## ADDED Requirements

### Requirement: Relay-doubt detection needs a relay-specific signal
A reply SHALL count as relay doubt only when it contains a relay-specific phrase (e.g. "relayed text", "playing along", "fabricated", "no real tool execution") and at least one further doubt signal; answers that only mention a prompt injection or uncertainty SHALL pass through.

#### Scenario: model reports an injection in a file
- **WHEN** a reply says a file "contains a prompt injection" and that the model "can't verify" its author
- **THEN** the reply reaches the client unchanged
