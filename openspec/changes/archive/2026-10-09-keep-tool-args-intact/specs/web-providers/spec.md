## ADDED Requirements

### Requirement: Gemini Web self-links are collapsed
The Gemini Web adapter SHALL replace markdown links whose label equals their URL, with or without the `http(s)://` scheme, by the label, in both streamed deltas and the final text, so tool-call arguments keep the literal path or URL the model wrote.

#### Scenario: Self-linked path in a tool call
- **WHEN** Gemini returns `{"file_path":"~/x/[github.com/o/r/p.json](https://github.com/o/r/p.json)"}` inside a `<tool_call>`
- **THEN** the tool call carries `~/x/github.com/o/r/p.json`
