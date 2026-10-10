## ADDED Requirements

### Requirement: Web catalog marks optional and enumerated arguments
The web tool catalog SHALL list required arguments as `name:type`, optional arguments as `name?:type`, and enumerated arguments by their allowed values joined with `|`. Before a parsed web tool call is returned to the client, optional arguments whose value is null or outside the schema enum SHALL be removed.

#### Scenario: ChatGPT fills Agent isolation with an invalid value
- **WHEN** a web reply calls `Agent` with `{"prompt":"count lines","description":"d","isolation":"none"}` and `isolation` is an optional enum of `worktree|remote`
- **THEN** the tool call returned to the client has no `isolation` argument and keeps `prompt` and `description`

### Requirement: Task cue ignores skill bodies
When the gateway restates the current task for a web model, it SHALL use the latest user turn that is not a skill expansion (a turn starting with `Base directory for this skill:`).

#### Scenario: Skill loaded mid-task
- **WHEN** the history is a user request, a `Skill` tool call, and the injected skill body
- **THEN** the restated task is the user request
