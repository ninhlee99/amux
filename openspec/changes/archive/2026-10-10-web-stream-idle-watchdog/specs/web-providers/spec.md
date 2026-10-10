## ADDED Requirements

### Requirement: Web streams have an idle timeout
Web provider HTTP responses SHALL fail with an "upstream stream idle" error when no bytes arrive for 3 minutes, so a silent upstream does not hold the client's request open indefinitely. Streams that keep delivering bytes SHALL NOT be limited in total duration.

#### Scenario: claude.ai goes silent mid-completion
- **WHEN** a Claude Web completion stream sends headers and then no data for 3 minutes
- **THEN** the adapter's read fails with the idle error and the request returns an error instead of hanging
