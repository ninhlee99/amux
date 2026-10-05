## Context

During prolonged autonomous agent sessions across various IDEs (Claude Code, Cursor, Codex, Antigravity), tool execution must remain deterministic. When web providers (ChatGPT Web, Claude Web, Gemini Web) output conversational prose or refusals, heuristic attempts to infer shell commands from backticks or unquoted prose can lead to unexpected side-effects or infinite tool loops.

## Goals / Non-Goals

**Goals:**
- Guarantee that tool calls from web backends are parsed solely from structured markup formats (`<tool_call>`, `[tool_call]`, `<<<AMUX_TOOL>>>`, and markdown fences labeled `tool_call` or `json` with explicit name/argument keys).
- Strip tool markup cleanly from user-visible streaming text so clients receive pristine assistant responses.
- Ensure shutdown (`amux stop`, `amux off`) and unhooking procedures cleanly reset all IDE configurations to native state without leaving dangling network proxies.

**Non-Goals:**
- Altering the core protocol bridge formats (`/v1/messages`, `/v1/chat/completions`, `/v1/responses`, `/v1beta`).
- Modifying Keychain or Vault encryption layers.

## Decisions

- **Decision 1: Strict Explicit Tool Markup over Prose Guessing**:
  - *Rationale*: Guessing commands like `git status` or file paths from natural language sentences introduces non-deterministic loops when models provide explanations or disclaimers.
  - *Alternatives considered*: Keeping fuzzy regex matches. Rejected because of hallucination risks.

- **Decision 2: Comprehensive Cleanup on Teardown**:
  - *Rationale*: Developers should never experience broken IDE networking after running `amux stop` or `amux off`. All environment overrides and IDE settings files are restored idempotently.

## Risks / Trade-offs

- [Risk] Web models that do not output any markup will not execute tools → [Mitigation] Models receive clear, concise catalog preambles instructing them to emit `<tool_call>` when tool execution is required.
