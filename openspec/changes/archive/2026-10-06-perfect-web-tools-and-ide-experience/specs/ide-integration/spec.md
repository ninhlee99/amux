## ADDED Requirements

### Requirement: Application bundle and emerging agent detection in doctor
`amux doctor` SHALL inspect macOS `/Applications` for installed IDE bundles (Cursor, Windsurf, Zed, VS Code) in addition to PATH lookups, and SHALL report availability for emerging coding agents including `aider` and `opencode`.

#### Scenario: Windsurf and Cursor detection via Application Bundle
- **WHEN** Cursor or Windsurf is installed in `/Applications` but the CLI binary is not present in shell PATH
- **THEN** `amux doctor` detects the application and reports it as Available

### Requirement: Automated GUI IDE settings configuration
`amux run cursor --setup` and `amux run windsurf --setup` SHALL automatically configure the local IDE settings (`settings.json`) with the AMUX Gateway OpenAI base URL without requiring manual user GUI navigation.

#### Scenario: One-command Cursor setup
- **WHEN** user executes `amux run cursor --setup`
- **THEN** AMUX updates Cursor's settings with `cursor.general.openaiBaseUrl` pointing to `:8787/v1` before launching the editor
