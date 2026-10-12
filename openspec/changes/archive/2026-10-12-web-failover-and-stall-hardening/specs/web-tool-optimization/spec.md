## ADDED Requirements

### Requirement: Client harness metadata is condensed for web models
Web prompts SHALL replace Claude Code's "Async agent launched" tool result with one line naming the agent id, and each `<task-notification>` (with its system-notification wrapper) with `[Background task <id> <status>] <summary>` plus its result.

#### Scenario: background agent completes
- **WHEN** the history holds an "Async agent launched" result and a task notification with result `3`
- **THEN** the web prompt shows "Background subagent started (id …)" and "[Background task … completed] … Result: 3", without the harness directives

### Requirement: Web models wait for background agents
While a launched background agent has not reported back, the web prompt SHALL tell the model that the result arrives as a notification and not to poll for it.

#### Scenario: agent still running
- **WHEN** a tool result says an async agent launched and no task notification followed
- **THEN** the prompt closer contains the do-not-poll note

### Requirement: Claude Web preamble describes the relay
The Claude Web tool preamble SHALL state that the conversation is relayed from the user's coding-agent client, which runs `<tool_call>` blocks on the user's machine and pastes real output back, separate from claude.ai's own sandbox tools.

#### Scenario: claude.ai receives a tool result
- **WHEN** a Claude Web prompt carries tools
- **THEN** it begins with the relay description
