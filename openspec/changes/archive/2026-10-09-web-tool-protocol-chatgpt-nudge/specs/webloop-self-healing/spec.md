## ADDED Requirements

### Requirement: One nudge retry on web refusal or stall
For a request with client tools, when a web adapter's reply contains no tool call parsed from the model's own text and the reply is a tool refusal or a stall (a short reply announcing an action without a `<tool_call>`), the gateway SHALL re-send the turn once with an added user turn that restates the `<tool_call>` protocol and the current task, and SHALL return only the retried reply to the client. Thread checkpoints SHALL fingerprint only the client's messages.

#### Scenario: ChatGPT stalls mid-task
- **WHEN** ChatGPT Web replies "I need to continue by running the remaining required tool steps." with no tool call
- **THEN** the gateway re-sends once with the nudge turn, and the client receives the retried reply's tool call

#### Scenario: Answer passes through
- **WHEN** a web reply is a final answer without tool calls and is neither a refusal nor a stall
- **THEN** it is returned unchanged and no retry is sent
