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
