## ADDED Requirements

### Requirement: One account per product per email
amux SHALL keep at most one account row per product and email, where products are ChatGPT Web, Codex, Claude Web, Claude Code, Antigravity, Gemini Web and Gemini API. Accounts of different products with the same email MUST stay separate rows, and a login MUST NOT take over or overwrite another product's row.

#### Scenario: ChatGPT Web login with a Codex subscription on the same email
- **WHEN** a user with a Codex subscription for `me@x.com` runs `amux login chatgpt` signed in as `me@x.com`
- **THEN** a ChatGPT Web row for `me@x.com` is saved under a `chatgpt:` id and the Codex row is unchanged

#### Scenario: Relogin of an account saved without email
- **WHEN** a ChatGPT Web row was saved without an email and the same account logs in again
- **THEN** that row learns its email and is updated in place; no second row is added

### Requirement: Active browser session is used
When auto-extracting a web session cookie, amux SHALL choose the most recently used unexpired session across all supported browsers and profiles.

#### Scenario: Two profiles signed in
- **WHEN** Edge "Default" and "Profile 2" are both signed in to chatgpt.com and Profile 2 was used last
- **THEN** the Profile 2 session is imported

### Requirement: Accounts listed by provider and email
`amux accounts` and the table printed after a login SHALL show the provider name and account email rather than internal ids, and account commands SHALL accept an email, `provider:email`, or the row number.

#### Scenario: Email on two providers
- **WHEN** a user runs `amux switch me@x.com` and that email is on Codex and ChatGPT Web
- **THEN** amux names both choices (`codex:me@x.com`, `chatgpt:me@x.com`) instead of picking one silently
