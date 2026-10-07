## Why

`amux login chatgpt` for an email that also had a Codex subscription landed on the Codex row (`codex:01`), overwrote it with a ChatGPT Web credential, and left an anonymous `chatgpt:01` behind — three rows for one person, two with the same ID. The rule "one account per email per canonical provider (openai)" treated ChatGPT Web and Codex as the same thing. Cookie auto-extract also took the first browser profile found, not the one the user is signed in to now, and account lists showed internal IDs (`codex:01`) instead of what users recognise.

## What Changes

- One row per **product** and email (ChatGPT Web, Codex, Claude Web, Claude Code, Antigravity, Gemini Web, …) instead of per canonical provider; a web login never takes or overwrites a subscription's ID.
- ChatGPT Web login backfills the email of rows saved without one, so a relogin updates that row.
- Browser cookie extraction picks the most recently used unexpired session across all browsers/profiles; only the chosen Chromium profile is decrypted.
- `amux accounts` (also printed after every login) shows `# | PROVIDER | ACCOUNT | …` without internal IDs; commands accept an email, `provider:email`, or row number; an email on several providers asks which one.
- No refresh-token prompt after a successful session-cookie login.

## Impact

- `pkg/provider/pool_slot.go`, `pkg/provider/config.go`, `pkg/provider/dedup.go`, `pkg/identity/*`, `pkg/browser/cookies.go`, `pkg/ui/login.go`, `pkg/cli/id.go`, `pkg/cli/pool.go`.
