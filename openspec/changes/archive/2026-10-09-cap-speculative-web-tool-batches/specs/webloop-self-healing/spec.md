## ADDED Requirements

### Requirement: Speculative tool batches are capped
When one web reply yields more than 5 tool calls, the gateway SHALL forward only the first 5 and SHALL NOT forward the reply's text to the client, because text written alongside a scripted batch describes results no tool produced.

#### Scenario: Gemini scripts the whole review in one reply
- **WHEN** a Gemini Web reply holds "Review published successfully" followed by 12 `<tool_call>` blocks
- **THEN** the client receives 5 tool calls and none of that text

### Requirement: Lost-task replies mid tool loop are nudged
When the client's last turn is a tool result in a session that already ran tools, and a web reply without tool calls has forgotten the task (a greeting, "How can I help you today?", "please provide the task"), the gateway SHALL treat it like a stall and re-send once with the nudge turn. As a reply to a new user message it SHALL pass through unchanged.

#### Scenario: Gemini derails after reading a diff of prompts
- **WHEN** after a `git diff` tool result Gemini Web replies "Understood. I am ready to assist you… How can I help you today?"
- **THEN** the gateway re-sends once with the nudge turn restating the task
