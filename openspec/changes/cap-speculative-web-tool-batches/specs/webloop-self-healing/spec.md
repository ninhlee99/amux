## ADDED Requirements

### Requirement: Speculative tool batches are capped
When one web reply yields more than 5 tool calls, the gateway SHALL forward only the first 5 and SHALL NOT forward the reply's text to the client, because text written alongside a scripted batch describes results no tool produced.

#### Scenario: Gemini scripts the whole review in one reply
- **WHEN** a Gemini Web reply holds "Review published successfully" followed by 12 `<tool_call>` blocks
- **THEN** the client receives 5 tool calls and none of that text
