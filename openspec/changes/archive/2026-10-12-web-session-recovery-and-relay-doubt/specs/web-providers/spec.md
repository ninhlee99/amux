## ADDED Requirements

### Requirement: Browser refresh finds the account's own profile
When a web adapter refreshes its session from installed browsers, it SHALL consider the session cookie from every browser profile, most recently used first, and adopt the first one signed in to the adapter's configured account, together with that profile's companion cookies. With no configured account the most recent working session SHALL be used. If no profile holds the account, the refresh SHALL fail with an authentication error naming the accounts found.

#### Scenario: two ChatGPT accounts in two Edge profiles
- **WHEN** `chatgpt:bi117ute`'s stored token expired and Edge holds ninhle21199 (used last) and bi117.ute sessions
- **THEN** the adapter adopts the bi117.ute session and the request succeeds
