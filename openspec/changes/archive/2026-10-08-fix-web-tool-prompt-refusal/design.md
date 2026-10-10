## Context

AMUX allows IDE coding assistants (Claude Code, Cursor, Codex, AGY) to use web-based LLM accounts (Claude Web, ChatGPT Web, Gemini Web) through WebLoop 2.0 text emulation. Web models lack native function calling APIs, so AMUX provides a tool catalog and formatting instructions in the prompt.

Currently, `pkg/tools/webloop.go` and `pkg/provider/prompt.go` use coercive instructions designed to prevent models from saying they lack tool access:
- "never claim lack of tools / ask to paste / fake edits"
- "DO NOT decline or claim you lack tools/repo access"
- "DO NOT claim you cannot commit or edit files directly"
- "Never refuse or ask user to paste / no 'lack of tools'"

While originally intended to nudge ChatGPT Web and Gemini Web to call tools, aligned models—especially Claude 3.5 and 3.7 on `claude.ai`—interpret these coercive instructions as adversarial prompt injection and safety bypass attempts. Consequently, Claude Web refuses to emit tool calls and instead produces lengthy meta-discussions questioning the harness.

## Goals / Non-Goals

**Goals:**
- Eliminate adversarial and coercive framing from web prompt templates.
- Maintain clear, authoritative function-calling instructions and few-shot examples so ChatGPT Web and Gemini Web continue to call tools reliably.
- Enhance `IsToolRefusal` keyword recognition to catch meta-simulation refusal text from Claude Web.
- Ensure all existing tests in `pkg/tools` and `pkg/provider` continue to pass.

**Non-Goals:**
- Rewriting the WebLoop 2.0 architecture or changing how native API providers handle tools.
- Forcing synthetic tool calls when the user's intent was conversational or when a model legitimately has no applicable tools.

## Decisions

### Decision 1: Neutral, Professional Tool Protocol Specification
Instead of commanding the model what *not* to say ("never refuse", "fake edits are forbidden"), we define the environment neutrally as an agent with external tool integration:
- **Preamble**: State that environment tools are available to inspect, read, edit, and execute commands on the workspace. Show the catalog and format: `<tool_call>{"name":"...","arguments":{...}}</tool_call>`.
- **Reminder**: Reinforce that workspace actions must be initiated by outputting `<tool_call>` blocks, then awaiting `[Tool result]`.
- **Closer**: State that when additional workspace actions, tests, or edits are required, output `<tool_call>` before concluding.

*Rationale*: Aligned models like Claude follow positive protocol definitions eagerly, but trigger safety refusals when prompts attempt to explicitly suppress refusal capabilities.

### Decision 2: Multi-Model Compatibility Guardrails
- **ChatGPT Web**: Retain the note that built-in container/Python tools cannot reach the workspace, ensuring it emits `<tool_call>` instead of trying to run local Python code in OpenAI's sandbox.
- **Gemini Web**: Retain multi-schema support and clear XML `<tool_call>` examples so Gemini's stream parser continues to extract calls.

### Decision 3: Expand `IsToolRefusal` with Simulation & Harness Phrases
Add keywords such as:
- `text-based simulation`
- `not actually claude code`
- `in a chat interface`
- `fake tool calls`
- `external harness`
- `prompt injection`

## Risks / Trade-offs

- [Risk] ChatGPT Web might occasionally revert to conversational "I cannot access your files" if phrasing is too soft.
  → *Mitigation*: Keep clear positive imperatives ("To read files or run commands, output `<tool_call>` and await execution results") and the explicit 1-shot `<tool_call>` example.
- [Risk] Unit tests asserting on exact prompt strings may fail.
  → *Mitigation*: Audit and update test assertions in `prompt_test.go` and `webloop_test.go`.
