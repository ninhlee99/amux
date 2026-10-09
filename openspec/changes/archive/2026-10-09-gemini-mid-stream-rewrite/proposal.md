## Why

Reviewing PR 47 through Gemini Web, the review came back as `<tool_call> {"name":"Bash","arguments":{"command":"sh \"…/cache### 🤖【AI REVIEW】Overview…` and `Toàn bộ các### 🤖【AI REVIEW】Overview…`. Raw StreamGenerate frames show Gemini restarting an answer mid-stream: a later snapshot shrinks and diverges from the earlier one. The adapter kept the longest snapshot as the answer and streamed deltas by offset, so the abandoned draft was glued to the rewrite and an unfinished tool call swallowed the review.

## What Changes

- The latest non-empty snapshot is the answer (not the longest).
- With client tools, deltas are not streamed (the turn is parsed whole and held by `sendWithWebNudge`); only the final text is sent.
- Without tools, deltas stream while the snapshot still extends what was sent; after a rewrite the final answer follows the draft after a blank line instead of being spliced into it.

## Capabilities

### Modified Capabilities
- `web-providers`: Gemini mid-stream rewrites.

## Impact

- `pkg/provider/gemini_web.go`.
