# web-providers Specification

## Purpose
Web chat accounts (ChatGPT, Claude.ai, Gemini web) serve as free-quota backends using the user's own browser sessions.
## Requirements
### Requirement: Dedicated login profiles
Web logins SHALL happen in a dedicated Chromium profile under `~/.amux/browser-profiles/<name>`, never the user's system browser profile, with the session cookie captured over the DevTools Protocol.

#### Scenario: Claude web login
- **WHEN** the user runs `amux login claude` and chooses web
- **THEN** a separate browser window opens and amux stores `sessionKey` once it appears

### Requirement: Per-project conversation threads
Web adapters SHALL keep one server-side conversation per project, client session and system prompt: the thread key is the project plus a hash of the session id and the leading system turns (the system prompt before the first non-system message), ignoring volatile billing header lines and per-turn system notes later in the conversation. A thread is reused until a turn/token budget or an upstream 404/limit forces a new one; requests marked `FullContext` MUST run on a fresh, stateless thread. The first turn of a thread SHALL carry the client's system prompt and full transcript; later turns on a live thread SHALL carry only what the thread has not seen (the new user request, or the tool results since the last reply) and MUST NOT re-send the system prompt. Resetting a project SHALL clear all of its session threads.

#### Scenario: Thread rotation
- **WHEN** a project's thread reaches its turn limit
- **THEN** the next request opens a new thread with the full flattened context

#### Scenario: New client session
- **WHEN** the user opens a new Claude Code session in a project whose earlier session has a live ChatGPT thread
- **THEN** the first request of the new session opens a new thread carrying its system prompt

#### Scenario: Side request with another system prompt
- **WHEN** Claude Code sends a title-generation request with the same session id but a different system prompt
- **THEN** it runs on its own thread and the session's main thread keeps its system prompt

#### Scenario: Per-turn system notes
- **WHEN** Claude Code adds a new `<total_tokens>` system note on each turn of a session
- **THEN** every turn stays on the session's thread

#### Scenario: Follow-up on a live thread
- **WHEN** the user sends "improve it" after a finished tool session on a live thread
- **THEN** the prompt holds only that request and the tool-call cue, not the system prompt or earlier turns

### Requirement: Automatic session refresh
When a web session expires the adapter SHALL try to refresh it once (dedicated profile first, then other supported sources) and otherwise fail with an authentication error the pool can fail over on. In keychain-free mode cookie refresh MUST skip browser cookie stores whose encryption key lives in the OS keychain (Chromium family) and SHALL read the session from amux's own login profile over the DevTools Protocol instead.

#### Scenario: Expired cookie
- **WHEN** Gemini web answers with an auth error
- **THEN** the adapter refreshes cookies once and retries, else returns `ErrAuthentication`

#### Scenario: Keychain-free refresh
- **WHEN** the secret store is `file` and ChatGPT web needs a fresh session cookie
- **THEN** no "Chrome Safe Storage" keychain item is read and the cookie comes from `~/.amux/browser-profiles/chatgpt`

### Requirement: Thread turn and token hygiene
Web adapters SHALL track accumulated prompt tokens and turn counts per conversation thread and automatically rotate to a fresh thread when limits are reached or when context pollution is detected.

#### Scenario: Token limit thread rotation
- **WHEN** an ongoing web conversation reaches the configured token threshold
- **THEN** the adapter rotates the project conversation to a clean state for subsequent requests

### Requirement: Gemini web account identity detection
Gemini web login SHALL extract the user's account email and subscription tier from the authenticated session at `gemini.google.com/app`.

#### Scenario: Gemini web login email detection
- **WHEN** the user completes login to Gemini Web
- **THEN** amux loads `https://gemini.google.com/app` with the session cookies, extracts the user's email, and saves the account under `gemini:web:<email_prefix>`

### Requirement: Gemini Web self-links are collapsed
The Gemini Web adapter SHALL replace markdown links whose label equals their URL, with or without the `http(s)://` scheme, by the label, in both streamed deltas and the final text, so tool-call arguments keep the literal path or URL the model wrote.

#### Scenario: Self-linked path in a tool call
- **WHEN** Gemini returns `{"file_path":"~/x/[github.com/o/r/p.json](https://github.com/o/r/p.json)"}` inside a `<tool_call>`
- **THEN** the tool call carries `~/x/github.com/o/r/p.json`

### Requirement: Gemini mid-stream rewrites
The Gemini Web adapter SHALL take the latest non-empty response snapshot as the answer. For requests with client tools it SHALL send only that final text. For requests without tools it SHALL stream text only while each snapshot extends what was already sent, and SHALL send the final answer after a blank line when a rewrite diverged from it.

#### Scenario: Draft abandoned for a rewrite
- **WHEN** Gemini streams `<tool_call> {"name":"Ba…` and then restarts with `## Review\nAll good.`
- **THEN** the answer is `## Review\nAll good.` with no part of the draft spliced into it

### Requirement: Web streams have an idle timeout
Web provider HTTP responses SHALL fail with an "upstream stream idle" error when no bytes arrive for 3 minutes, so a silent upstream does not hold the client's request open indefinitely. Streams that keep delivering bytes SHALL NOT be limited in total duration.

#### Scenario: claude.ai goes silent mid-completion
- **WHEN** a Claude Web completion stream sends headers and then no data for 3 minutes
- **THEN** the adapter's read fails with the idle error and the request returns an error instead of hanging

### Requirement: Dead web connections are detected quickly
Web provider transports SHALL health-check idle HTTP/2 connections with pings so a silently dropped connection is discarded within about 45 seconds, and the ChatGPT sentinel request SHALL fail after 60 seconds instead of waiting for the operating system's TCP timeout.

#### Scenario: Pooled connection dropped by the network
- **WHEN** the HTTP/2 connection to chatgpt.com stops answering while idle
- **THEN** the next request uses a new connection rather than hanging for many minutes

### Requirement: ChatGPT python tool runs on the client
When the client sent tools and ChatGPT Web addresses code to its own `python` tool, the adapter SHALL return a Bash tool call that runs that code with `python3` on the client instead of ChatGPT's tool error or canned "can't do more advanced data analysis" reply.

#### Scenario: Python tool unavailable on the account
- **WHEN** ChatGPT streams a `code` message to `python`, then a `system_error`, then "It seems like I can’t do more advanced data analysis right now."
- **THEN** the client receives one Bash tool call running the code and none of that text

### Requirement: Gemini Web detects requests that never start
The Gemini Web adapter SHALL treat a stream as stalled when no conversation-id or text frame arrives within 45 s of sending (including the wait for response headers), or no further progress arrives for 3 minutes; heartbeat frames SHALL NOT count as progress. A stalled request SHALL be retried once in a new Gemini chat, then fail with an error.

#### Scenario: heartbeat-only stream
- **WHEN** Gemini sends only `{"37":[n]}` frames after the headers
- **THEN** the adapter abandons the stream after 45 s and retries in a new chat

### Requirement: Gemini canned failures are errors
A short reply that is one of Gemini's generic failure texts ("Sorry, something went wrong…", "I seem to be encountering an error…") SHALL be handled as an error (retry once in a new chat), and SHALL NOT be streamed to the client.

#### Scenario: canned failure
- **WHEN** Gemini answers "Sorry, something went wrong. Please try your request again."
- **THEN** nothing is streamed and the request is retried in a new chat

### Requirement: Web limit errors carry the reset time
Claude Web and ChatGPT Web rate-limit errors SHALL carry the upstream reset time (`Retry-After` header, or claude.ai `resetsAt`) as the error's RetryAfter.

#### Scenario: ChatGPT 429 with Retry-After
- **WHEN** ChatGPT answers 429 with `Retry-After: 600`
- **THEN** the returned error is a rate-limit error with RetryAfter 10 minutes

### Requirement: Refreshed sessions must belong to the configured account
When a web adapter refreshes its session from the shared browser jar, it SHALL check the refreshed session's email against the account's configured email and reject a different account (authentication error, previous cookies kept). Gemini SHALL persist browser-refreshed cookies only after this check passes.

#### Scenario: browser signed in to another ChatGPT account
- **WHEN** `chatgpt:a` (a@x.com) refreshes and the browser session is b@x.com
- **THEN** the refresh fails with an authentication error and a@x.com's slot keeps its own credentials

### Requirement: Expired ChatGPT access tokens are refreshed before the sentinel call
The ChatGPT Web adapter SHALL refresh an access token whose JWT `exp` has passed before sending, and SHALL refresh and retry once when the sentinel endpoint answers 401 / `token_expired`; a second failure SHALL be an authentication error.

#### Scenario: access token expired overnight
- **WHEN** the stored access token's exp is in the past
- **THEN** the adapter refreshes it from the session cookie before calling the sentinel endpoint

### Requirement: Browser refresh finds the account's own profile
When a web adapter refreshes its session from installed browsers, it SHALL consider the session cookie from every browser profile, most recently used first, and adopt the first one signed in to the adapter's configured account, together with that profile's companion cookies. With no configured account the most recent working session SHALL be used. If no profile holds the account, the refresh SHALL fail with an authentication error naming the accounts found.

#### Scenario: two ChatGPT accounts in two Edge profiles
- **WHEN** `chatgpt:bi117ute`'s stored token expired and Edge holds ninhle21199 (used last) and bi117.ute sessions
- **THEN** the adapter adopts the bi117.ute session and the request succeeds

### Requirement: Gemini throttling rests the account
When a Gemini Web request stalls and its retry on a fresh chat stalls as well, the adapter SHALL fail with a rate-limit error carrying a 5-minute RetryAfter.

#### Scenario: latency keeps climbing
- **WHEN** two consecutive Gemini attempts make no progress in time
- **THEN** the router cools the account down and serves the turn from another account

### Requirement: ChatGPT citation markers are removed
ChatGPT Web text SHALL NOT contain its private-use citation markers (U+E200 … U+E201).

#### Scenario: web search answer
- **WHEN** ChatGPT answers with `citeturn0search0`
- **THEN** the client receives the answer without the marker

