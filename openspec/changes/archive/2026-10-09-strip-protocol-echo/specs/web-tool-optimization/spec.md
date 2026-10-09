## ADDED Requirements

### Requirement: Echoed protocol lines are not shown
Text the client receives from a web reply SHALL NOT contain lines that only repeat the gateway's tool protocol instructions, such as "Wait for [Tool result] before continuing.".

#### Scenario: Gemini echoes the wait line
- **WHEN** a Gemini Web reply is "Wait for [Tool result] before continuing." followed by a `<tool_call>`
- **THEN** the client receives the tool call and no text
