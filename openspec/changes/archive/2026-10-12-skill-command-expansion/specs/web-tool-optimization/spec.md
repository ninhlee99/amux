## ADDED Requirements

### Requirement: Commands loaded through the Skill tool are skill bodies
A user turn that directly follows a `Launching skill:` tool result SHALL be treated as a loaded skill body even without a "Base directory" header: it SHALL carry the skill-loaded note and SHALL NOT be taken as the user's current task.

#### Scenario: plugin command loaded mid-task
- **WHEN** the model calls Skill for `dev-workflow:status` during an eight-step request
- **THEN** the web prompt marks the command body as a loaded skill and the mid-task cue still names the eight-step request
