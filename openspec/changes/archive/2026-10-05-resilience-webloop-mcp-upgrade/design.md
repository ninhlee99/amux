## Context

AMUX functions as a multi-tier AI Gateway and tool runner. Developers report that when tools are hooked to AMUX, if the gateway encounters router exhaustion or network errors, the client IDE immediately fails with connection aborts. Additionally, web-loop tool emulation requires heightened resilience against unescaped characters in bash commands and multi-argument JSON blobs, while MCP clients need deeper contextual code review tools.

## Goals / Non-Goals

**Goals:**
- Provide transparent fail-safe forwarding when pool accounts fail or encounter exhaustion.
- Enhance fuzzy JSON argument repair and schema property coercion in `pkg/tools/webloop.go` to handle complex multi-line bash scripts and diffs.
- Provide `amux_review` in the MCP suite for multi-file code diff review, and support context metadata in `amux_ask`.
- Ensure backwards compatibility with all existing IDE clients (Claude Code, Cursor, Codex, Antigravity).

**Non-Goals:**
- Replacing IDE-level execution engine (tools continue to be executed locally by the IDE agent).
- Bypassing provider safety policies.

## Decisions

### Decision 1: Gateway Fail-Safe Pass-Through
- When the gateway receives a request for `/v1/messages` and all pool accounts are in cooldown/exhausted, rather than immediately terminating with 503/500, the gateway checks for available native subscription credentials in the keychain/environment and proxies directly upstream as a safety net.
- *Alternatives considered*: Returning immediate HTTP 503. Rejected because it terminates active IDE agent turns unexpectedly.

### Decision 2: Streaming Fuzzy JSON Normalization in Web Loop
- Expand `repairJSON` and `parseToolCallJSON` with bracket balance recovery, unescaped quote scanning within string literals, and typed parameter remapping.
- *Alternatives considered*: Strict JSON parsing only. Rejected because web models frequently omit escape slashes on quotes inside shell command strings.

### Decision 3: MCP 2.0 Extended Tools
- Register `amux_review` tool in `pkg/mcp/tools.go` accepting `diff`, `context`, `focus` to allow second-opinion code review routed through the pool (including Meta Muse / Gemini Pro).
- Extend `amux_ask` to accept optional `context` string representing workspace state.

## Risks / Trade-offs

- [Risk: Upstream Direct Token Consumption] → Mitigation: Direct fail-safe only engages when all configured pool accounts are exhausted and native credentials exist.
- [Risk: Malformed JSON false positives] → Mitigation: Rigorous unit tests covering diverse tool patterns (`Bash`, `edit_file`, `replace_file_content`, `view_file`).
