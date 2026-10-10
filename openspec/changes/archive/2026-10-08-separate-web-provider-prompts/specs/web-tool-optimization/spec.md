## ADDED Requirements

### Requirement: Provider-specific web tool prompt specialization
The gateway SHALL tailor web tool prompt preambles, reminders, and closers specifically to the active web provider:
- For Claude Web (`claude`): The prompt SHALL instruct the model neutrally on tool invocation without referencing Python or container sandboxes.
- For ChatGPT Web (`chatgpt`): The prompt SHALL advise the model that internal Python/container tools cannot access the workspace, directing execution to `<tool_call>`.
- For Gemini Web (`gemini`): The prompt SHALL provide workspace tool directives aligned with Google model formats.

#### Scenario: Claude Web prompt omits container sandbox text
- **WHEN** formatting a prompt for Claude Web
- **THEN** the preamble and reminder do not contain references to Python or container sandboxes

#### Scenario: ChatGPT Web prompt retains sandbox steering
- **WHEN** formatting a prompt for ChatGPT Web
- **THEN** the preamble and reminder advise the model that internal python/container tools cannot access the workspace
