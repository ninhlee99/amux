## MODIFIED Requirements

### Requirement: Dynamic micro few-shot prompt injection
When formatting requests with tools for web chat sessions, the gateway SHALL include a concise 1-shot example demonstrating tool calling markup syntax to guide the web model, and SHALL use neutral, non-adversarial protocol framing without coercive phrases (such as "never refuse", "never claim lack of tools", "DO NOT decline", "fake edits are forbidden") that trigger model safety refusals.

#### Scenario: Tool catalog and few-shot formatting
- **WHEN** a client sends a ChatRequest containing tools to a Web backend
- **THEN** the injected prompt includes both the catalog and a 1-shot `<tool_call>` example using neutral technical protocol instructions

## ADDED Requirements

### Requirement: Robust web refusal detection without leaking simulation debate
The gateway SHALL detect web model refusal or simulation concerns (including indications of text simulation, harness distrust, or model inability to access tools) in `IsToolRefusal`, preventing raw refusal essays from degrading the user experience while ensuring valid tool calls and normal responses are preserved across Claude, ChatGPT, and Gemini web providers.

#### Scenario: Claude Web simulation refusal detected
- **WHEN** a web model response includes statements rejecting the session as a text simulation or expressing prompt injection suspicion
- **THEN** `IsToolRefusal` evaluates to true, allowing the gateway to handle the refusal appropriately
