## MODIFIED Requirements

### Requirement: Automatic session refresh
When a web session expires the adapter SHALL try to refresh it once (dedicated profile first, then other supported sources) and otherwise fail with an authentication error the pool can fail over on. In keychain-free mode cookie refresh MUST skip browser cookie stores whose encryption key lives in the OS keychain (Chromium family) and SHALL read the session from amux's own login profile over the DevTools Protocol instead.

#### Scenario: Expired cookie
- **WHEN** Gemini web answers with an auth error
- **THEN** the adapter refreshes cookies once and retries, else returns `ErrAuthentication`

#### Scenario: Keychain-free refresh
- **WHEN** the secret store is `file` and ChatGPT web needs a fresh session cookie
- **THEN** no "Chrome Safe Storage" keychain item is read and the cookie comes from `~/.amux/browser-profiles/chatgpt`
