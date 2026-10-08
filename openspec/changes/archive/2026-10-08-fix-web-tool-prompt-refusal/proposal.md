## Why

When IDE clients (like Claude Code or Cursor) invoke web models (especially Claude Web on `claude.ai`), the current web tool prompts in `pkg/tools/webloop.go` and `pkg/provider/prompt.go` use adversarial and coercive phrasing (e.g., "never claim lack of tools", "DO NOT decline", "fake edits are forbidden", "never refuse"). Modern aligned LLMs—particularly Claude 3.5 and Claude 3.7—detect these patterns as prompt injection / jailbreak attempts and explicitly refuse to execute tools, rejecting the simulated harness with explanatory refusal text.

We need to replace this coercive framing with standard, neutral function-calling protocol specifications, while preserving clear instructions and few-shot examples so that ChatGPT Web and Gemini Web continue to parse and execute `<tool_call>` blocks reliably without regression.

## What Changes

- **Neutral Tool Protocol Framing**: Replace adversarial phrases ("never refuse", "never claim lack of tools", "fake edits are forbidden", "DO NOT decline") in `webToolPreamble`, `webToolReminder`, `webToolCloser`, `midTaskCue`, and `newRequestCue` with standard, professional function calling protocol instructions.
- **Cross-Model Compatibility Preservation**: Retain clear guidance that repository inspection, command execution, and file edits occur via `<tool_call>` blocks, ensuring ChatGPT Web (with built-in Python sandbox) and Gemini Web (StreamGenerate) continue to emit valid tool markup.
- **Enhanced Refusal Detection**: Add Claude Web simulation/harness refusal patterns (`text-based simulation`, `fake tool calls`, `in a chat interface`, `not actually claude code`, `prompt injection`) to `IsToolRefusal` to properly handle and recover when models express reluctance.
- **Regression Testing**: Validate test suite across Claude, ChatGPT, and Gemini web scenarios to ensure zero regressions.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: Update prompt formatting requirements to specify neutral, non-adversarial tool calling protocol instructions that prevent safety refusals while maintaining multi-provider compatibility.

## Impact

- `pkg/tools/webloop.go`: Update `webToolPreamble`, `webToolReminder`, `webToolCloser`, and `IsToolRefusal`.
- `pkg/provider/prompt.go`: Update `midTaskCue`, `newRequestCue`, and closer formatting.
- Unit and emulation tests in `pkg/tools/` and `pkg/provider/`: Update string expectations and add regression tests for Claude Web, ChatGPT Web, and Gemini Web.
