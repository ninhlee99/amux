## Why

`/open-pr:review` through `amux run claude -p gemini:web:…` came back as one reply holding ~55 `<tool_call>` blocks — the whole workflow scripted up front with guessed arguments (a nonexistent commit sha, a worktree never created, a review id) — preceded by "Review published successfully, LGTM". Claude Code ran every call in parallel; nearly all failed, yet the user saw a success that never happened and nothing was posted.

## What Changes

- Web tool prompts allow several `<tool_call>` blocks only for independent calls (at most 5) and forbid writing results, ids or conclusions before the `[Tool result]`.
- Mid tool loop, a reply that forgot the task ("How can I help you today?") is nudged once, like a stall.
- A web reply with more than 5 tool calls keeps the first 5; the turn is marked speculative and the text streamed before the calls is discarded by the buffering consumer.

## Capabilities

### Modified Capabilities
- `webloop-self-healing`: cap speculative tool batches and drop their narration.

## Impact

- `pkg/tools/webloop.go`, `pkg/provider/web_nudge.go`, `pkg/types/chat.go`.
