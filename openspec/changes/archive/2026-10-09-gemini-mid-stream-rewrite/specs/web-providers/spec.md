## ADDED Requirements

### Requirement: Gemini mid-stream rewrites
The Gemini Web adapter SHALL take the latest non-empty response snapshot as the answer. For requests with client tools it SHALL send only that final text. For requests without tools it SHALL stream text only while each snapshot extends what was already sent, and SHALL send the final answer after a blank line when a rewrite diverged from it.

#### Scenario: Draft abandoned for a rewrite
- **WHEN** Gemini streams `<tool_call> {"name":"Ba…` and then restarts with `## Review\nAll good.`
- **THEN** the answer is `## Review\nAll good.` with no part of the draft spliced into it
