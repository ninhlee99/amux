## Why

Live runs of Claude Code through amux against Claude Web, ChatGPT Web and Gemini Web (Write/Bash/Edit, Skill, MCP `amux_status`, background `Agent`) found failures that unit tests missed:

- Gemini Web and ChatGPT Web report usage limits inside the stream, after `SendMessageStream` returned. The router never saw them: the account was recorded as healthy, stayed pinned, and the client got the error instead of another account.
- Gemini sometimes accepts a request and sends only `{"37":[n]}` heartbeats for ~6.5 minutes; the heartbeats defeat the byte-level idle watchdog. A subagent turn waited 219 s. Gemini also answers with canned failures ("Sorry, something went wrong…") that were passed on as answers.
- Web limits were cooled down for a fixed 2 minutes even when claude.ai/ChatGPT said when the limit resets.
- Migration stored Gemini Web on a paid plan as an OAuth subscription, so it was built as an Antigravity adapter.
- A failed pool fell back to the user's Claude Code login even when that subscription was off and outside the pool, and even for requests pinned to a web account.
- ChatGPT Web never refreshed an expired access token: the sentinel call failed with 401 before the refresh path ran.
- Browser-cookie refresh picked whichever account the shared browser was signed in to: `chatgpt:bi117ute` silently became `ninhle21199`, so two pool slots hit one account.
- claude.ai refused the task, calling real tool results "fabricated injection", after Claude Code's internal background-agent metadata ("internal ID - do not mention to user", "[SYSTEM NOTIFICATION - NOT USER INPUT]") was pasted into the chat. Web models also polled background agents with ListAgents/sleep loops instead of waiting for the notification.

## What Changes

- Router primes each stream up to its first content: an error before any output fails over like a synchronous error (cooldown + next account).
- Gemini Web: progress-based stall watch (45 s to the conversation ack, 3 min between progress frames), canned-error detection, one retry in a fresh chat, then error.
- Claude Web / ChatGPT Web limit errors carry the reset time (`resetsAt` / `Retry-After`) as `RateLimitError.RetryAfter`.
- Direct-credential fallback only for pooled, enabled subscriptions and never for pinned requests.
- ChatGPT Web refreshes expired tokens before / at the sentinel call.
- Browser-refreshed sessions are checked against the account's email (ChatGPT, Claude, Gemini).
- Web tier wins over a paid plan during migration; mis-stored web identities are repaired on load.
- Web prompts: Claude Code background-agent metadata is condensed to neutral facts; Claude Web preamble states the relay plainly; a cue tells the model not to poll a running background agent.

## Capabilities

### Modified Capabilities
- `provider-pool-routing`: in-stream failover, reset-aware cooldown.
- `web-providers`: Gemini stall / canned error handling, limit reset hints.
- `account-identity`: paid web plans stay web.
- `subscription-rotation`: no fallback to unpooled subscriptions.
- `web-tool-optimization`: harness metadata, relay intro, background-agent cue.

## Impact

- `pkg/router/prime.go`, `pkg/router/pool.go`, `pkg/provider/gemini_web.go`, `pkg/provider/claude_web.go`, `pkg/provider/chatgpt_web.go`, `pkg/provider/prompt.go`, `pkg/bridge/headers.go`, `pkg/provider/session_account.go`, `pkg/provider/config.go`, `pkg/identity/migrate.go`, `pkg/identity/store.go`, `pkg/tools/harness.go`, `pkg/tools/webloop.go`, `scripts/test_provider.sh`.
