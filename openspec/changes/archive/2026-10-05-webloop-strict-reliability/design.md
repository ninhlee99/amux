## Context

When AMUX acts as a gateway proxy for text-only web session backends (such as Claude Web, ChatGPT Web, Gemini Web), the gateway translates client `tools[]` definitions into system prompt instructions (preamble + catalog) and extracts tool calls from the model's generated text response.

In earlier versions, when models returned conversational refusals or incomplete task statements, AMUX attempted to guess user intents using regex lists ("needles") and forcibly generated synthetic `tool_call` blocks (such as `git diff` or `Read`). This introduced a severe "whack-a-mole" problem: adding new refusal keywords caused false positives on legitimate conversational responses, triggering unintended tool calls and causing IDE agents (Claude Code, Cursor, Codex) to crash or enter infinite rejection loops.

## Goals / Non-Goals

**Goals:**
- Eliminate fictitious tool call synthesis: never invent unprompted tool calls from natural language refusal or conversational prose.
- Provide clean, deterministic parsing for explicit tool call markups (`<tool_call>`, ````tool_call````, `[tool_call]`, `<<<AMUX_TOOL>>>`, ````json```` containing tool name/args).
- Deliver unparsed assistant text directly to the client IDE when no explicit tool markup is emitted, allowing the client IDE's agent logic to handle the response appropriately.
- Preserve argument coercion for fuzzy schema field matching (e.g. mapping `file_path` to `path` where appropriate).

**Non-Goals:**
- Creating new mock tools or modifying IDE-side tool execution.
- Emulating server-side execution of tools inside the gateway.

## Decisions

- **Decision: Strict explicit markup extraction only**
  - *Rationale*: Agent IDEs (such as Claude Code) have sophisticated conversation loop handling. When an LLM explains in natural language why it cannot proceed or what it needs, the IDE should receive that text directly rather than receiving an erroneous synthetic tool call.
  - *Alternatives considered*: Keeping heuristic forced extraction was rejected due to recurring regressions and false positive side-effects.

- **Decision: Retain schema key coercion**
  - *Rationale*: Web models may occasionally use synonyms like `path` instead of `file_path` or `cmd` instead of `command`. Normalizing argument keys against the live tool definition preserves compatibility without altering model intent.

## Risks / Trade-offs

- [Risk] Web model outputs plain text suggestion instead of tool call.
  → *Mitigation*: The client IDE receives the plain text, sees the suggestion, and naturally prompts the model in the next turn if needed.
