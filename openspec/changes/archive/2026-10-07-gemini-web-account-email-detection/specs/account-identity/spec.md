## MODIFIED Requirements

### Requirement: One account per product per email
amux SHALL keep at most one account row per product and email, where products are ChatGPT Web, Codex, Claude Web, Claude Code, Antigravity, Gemini Web and Gemini API. Accounts of different products with the same email MUST stay separate rows, and a login MUST NOT take over or overwrite another product's row.

#### Scenario: ChatGPT Web login with a Codex subscription on the same email
- **WHEN** a user with a Codex subscription for `me@x.com` runs `amux login chatgpt` signed in as `me@x.com`
- **THEN** a ChatGPT Web row for `me@x.com` is saved under a `chatgpt:` id and the Codex row is unchanged

#### Scenario: Relogin of an account saved without email
- **WHEN** a ChatGPT Web row was saved without an email and the same account logs in again
- **THEN** that row learns its email and is updated in place; no second row is added

#### Scenario: Gemini Web relogin with detected email
- **WHEN** a Gemini Web login is performed for `me@gmail.com`
- **THEN** it is saved under `gemini:web:me` and relogging in with the same email updates the existing row in place
