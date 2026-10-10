## ADDED Requirements

### Requirement: Skill metadata turn normalization
The gateway prompt engine SHALL filter IDE metadata blocks such as `<system-reminder>` when inspecting user messages, ensuring that trailing metadata updates containing `(no content)` are not misclassified as new user requests, and that the original user task goal (including slash commands and skills) is preserved across all subsequent tool turns.

#### Scenario: Trailing skill reminder does not break tool loop
- **WHEN** an IDE client appends a user message with `<system-reminder>15000000 tokens left</system-reminder>\n\n(no content)` following a tool execution
- **THEN** the prompt engine treats the session as an ongoing tool loop, does not inject new-request cues, and maintains the active task goal
