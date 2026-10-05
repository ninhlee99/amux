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
Web adapters SHALL reuse one server-side conversation per project until a turn/token budget or an upstream 404/limit forces a new thread; requests marked `FullContext` MUST run on a fresh, stateless thread.

#### Scenario: Thread rotation
- **WHEN** a project's thread reaches its turn limit
- **THEN** the next request opens a new thread with the full flattened context

### Requirement: Automatic session refresh
When a web session expires the adapter SHALL try to refresh it once (dedicated profile first, then other supported sources) and otherwise fail with an authentication error the pool can fail over on. In keychain-free mode cookie refresh MUST skip browser cookie stores whose encryption key lives in the OS keychain (Chromium family) and SHALL read the session from amux's own login profile over the DevTools Protocol instead.

#### Scenario: Expired cookie
- **WHEN** Gemini web answers with an auth error
- **THEN** the adapter refreshes cookies once and retries, else returns `ErrAuthentication`

#### Scenario: Keychain-free refresh
- **WHEN** the secret store is `file` and ChatGPT web needs a fresh session cookie
- **THEN** no "Chrome Safe Storage" keychain item is read and the cookie comes from `~/.amux/browser-profiles/chatgpt`

