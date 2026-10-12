## Why

Repeated live runs through the real gateway showed the pool never rotating:

- After the first automatic failover from Claude to the provider pool the gateway called `SetPreferred` (a manual pin) with the account it happened to use, so every later session stuck to it.
- Every bridge called `CheckSessionAccountSwitch(r, req, pool.Preferred() or "pool")` before routing. That pinned each session to the status-only "preferred" account, or to "pool"; with "pool" the router unpinned and re-picked round-robin, so a session hopped accounts each turn and its history was compacted to 6 messages every turn.
- Four Claude Code sessions started together all landed on `chatgpt:bi117ute` (the round-robin position advanced only after a stream's first chunk), which rate-limited it into a 45-minute quarantine.
- On a fresh AMUX_HOME the gateway snapshots the live Claude Code login as a profile without the user's "off" state, and used that subscription first.
- Gemini Web latency climbed 19 s → 58 s → 116 s → 164 s → stall on consecutive requests with no 429 (soft throttling); the router kept sending to it.
- ChatGPT leaked private-use citation markers (`citeturn0search0`) into answers.

## What Changes

- The round-robin slot is reserved when an account is picked.
- Automatic switches call `NoteAutoSwitch` (status only, no pin).
- Session-switch detection runs only for a manual pin (`bridge.SessionSwitched`); otherwise router affinity owns the session, and the router compacts once when a session must leave a dead pinned account.
- The rotator treats a profile whose Claude Code identity is turned off as off.
- A Gemini stall repeated on a fresh chat is a rate limit with a 5-minute cooldown.
- ChatGPT citation markers are stripped from streamed text.
- `scripts/test_provider.sh` appends to gateway.log; its `>` redirect clobbered lines the gateway itself appends.

## Capabilities

### Modified Capabilities
- `provider-pool-routing`: auto-switch never pins; router-owned session affinity.
- `subscription-rotation`: off identities keep snapshotted profiles off.
- `web-providers`: Gemini throttle detection, ChatGPT citation stripping.

## Impact

- `pkg/router/pool.go`, `pkg/proxy/server.go`, `pkg/proxy/rotator.go`, `pkg/bridge/{headers,openai,responses,gemini}.go`, `pkg/provider/gemini_web.go`, `pkg/provider/chatgpt_web.go`, `scripts/test_provider.sh`.
