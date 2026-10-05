## ADDED Requirements

### Requirement: Thread turn and token hygiene
Web adapters SHALL track accumulated prompt tokens and turn counts per conversation thread and automatically rotate to a fresh thread when limits are reached or when context pollution is detected.

#### Scenario: Token limit thread rotation
- **WHEN** an ongoing web conversation reaches the configured token threshold
- **THEN** the adapter rotates the project conversation to a clean state for subsequent requests
