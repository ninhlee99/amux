## ADDED Requirements

### Requirement: Elimination of synthetic refusal tool injection
The gateway SHALL NOT inject synthetic or heuristic tool calls when a web model outputs conversational refusal, incomplete work notices, or plain explanatory prose.

#### Scenario: Conversational response without tool markup
- **WHEN** a web model generates plain text explaining limitations or reviewing code without `<tool_call>` markup
- **THEN** the gateway streams the text verbatim to the client without synthesizing unprompted tool calls
