## MODIFIED Requirements

### Requirement: Isolated runtime runner
The CLI SHALL provide `amux run <command>` which launches any tool/IDE with temporary injected environment variables in a child process without mutating global shell profile files or launchctl. For Antigravity and Gemini CLI agents, it SHALL inject `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE`, and `GOOGLE_GENAI_BASE_URL`.

#### Scenario: Running client in sandbox wrapper
- **WHEN** user executes `amux run claude` or `amux run agy`
- **THEN** the child process receives the isolated proxy endpoints while parent shell and system settings remain pristine
