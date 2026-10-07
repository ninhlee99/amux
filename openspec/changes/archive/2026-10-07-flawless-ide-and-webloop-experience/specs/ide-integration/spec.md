## ADDED Requirements

### Requirement: Zero-pollution sandbox non-invasiveness
`amux run <ide>` SHALL NOT mutate persistent host settings files (`settings.json`, `config.toml`) unless the user explicitly passes the `--setup` flag.

#### Scenario: Running IDE without modifying disk settings
- **WHEN** user executes `amux run cursor` or `amux run windsurf` without `--setup`
- **THEN** AMUX launches the IDE with sandbox environment variables injected into the process only, leaving the persistent `settings.json` file untouched

### Requirement: Per-command provider flag in runner
`amux run <ide>` SHALL accept `--provider <id>` or `-p <id>` to route the sandboxed IDE session directly to a specified account or account family.

#### Scenario: Pinning provider in sandboxed session
- **WHEN** user executes `amux run claude -p gemini:web`
- **THEN** the injected gateway URL includes `/p/gemini:web` so all requests from that Claude session route directly to Gemini Web without mutating global proxy preferences
