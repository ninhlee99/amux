## MODIFIED Requirements

### Requirement: Per-project conversation threads
Web adapters SHALL reuse one server-side conversation per project until a turn/token budget or an upstream 404/limit forces a new thread; requests marked `FullContext` MUST run on a fresh, stateless thread with sanitized, generic concatenated prompts without injecting third-party task heuristics.

#### Scenario: Thread rotation
- **WHEN** a project's thread reaches its turn limit
- **THEN** the next request opens a new thread with the full flattened context without domain-specific hardcoded prefixes
