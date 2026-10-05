## ADDED Requirements

### Requirement: Full-spectrum coding MCP tools
The server SHALL expose `amux_diagnose` (error and stack trace investigation), `amux_fix` (code fix and patch generation), and `amux_analyze` (architecture and project structure analysis), routing requests through the account pool.

#### Scenario: Diagnose error
- **WHEN** `amux_diagnose` is called with `error: "panic: runtime error: nil pointer dereference"` and code context
- **THEN** an expert root-cause diagnosis is returned

#### Scenario: Fix bug
- **WHEN** `amux_fix` is called with `file_content` and `issue`
- **THEN** a corrected code patch is returned

#### Scenario: Analyze architecture
- **WHEN** `amux_analyze` is called with `structure` and `objective`
- **THEN** an architectural evaluation and recommendations are returned
