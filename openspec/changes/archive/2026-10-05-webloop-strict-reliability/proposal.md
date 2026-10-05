## Why

Web-Loop Tool Emulation in AMUX suffered from regression instability and false positives due to fragile string-matching heuristics ("whack-a-mole" needles) attempting to detect model refusals and forcibly invent fictitious tool calls (such as arbitrary `git diff` or `Read` calls). This caused infinite rejection loops and broken conversations in IDE coding agents (Claude Code, Cursor, Codex). A strict, robust, and clean extraction mechanism is required to ensure stability and predictability.

## What Changes

- **Eliminate forced tool hallucination**: Remove arbitrary guessing and forced generation of synthetic tool calls when natural language refusal or incomplete prose is returned.
- **Strict structured tool call parsing**: Retain robust parsing of explicit tool markup (`<tool_call>`, ````tool_call````, `[tool_call]`, `<<<AMUX_TOOL>>>`, ````json```` with name/args), while letting plain conversational text cleanly pass through to the IDE client.
- **Clean argument coercion**: Preserve schema argument name normalization (`path` vs `file_path`, `command` vs `cmd`) based on the active client tool catalog.
- **Refusal transparency**: If a web model cannot or does not invoke tools, its plain text response is delivered directly to the client IDE agent so the agent can naturally prompt or handle the response without corrupt state machine transitions.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `tool-dialects`: Clarify that web tool emulation extracts structured tool calls without inventing fictitious calls from plain conversational text.
- `web-tool-optimization`: Update prompt injection and response extraction requirements to prioritize strict structured parsing over heuristic forcing.

## Impact

- `pkg/tools/webloop.go`: Cleaned up parser and removed fragile refusal needle bloat and synthetic tool forcing.
- `pkg/tools/webloop_refusal_test.go` and `webloop_test.go`: Updated tests to verify clean text pass-through on refusal and accurate extraction on valid tool markup.
