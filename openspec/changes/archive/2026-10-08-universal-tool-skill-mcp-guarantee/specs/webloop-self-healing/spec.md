## ADDED Requirements

### Requirement: Multi-tier fallback tool call recovery
When a web model fails to output explicit `<tool_call>` markup in response to a request with tools defined, the tool parsing engine SHALL recover executable commands from markdown `bash`/`sh` codeblocks or inline backticks, preventing tool loops and skill execution from stalling.

#### Scenario: Recover markdown bash codeblock mid-task
- **WHEN** a web model outputs an executable command inside a markdown ```bash codeblock instead of `<tool_call>` tags
- **THEN** the parser extracts the command and emits a valid `Bash` tool call to continue execution

#### Scenario: Kickstart tool loop for explicit skill requests
- **WHEN** a user requests an agentic skill action (such as `/open-pr:fix` or `/open-pr:review`) and the model emits conversational prose without tool calls
- **THEN** the engine auto-initiates repository inspection to supply live tool results and maintain the agentic workflow
