## Why

When logging in to Gemini Web via `amux login gemini-web`, AMUX previously saved the account as an anonymous numeric sequence (e.g. `gemini:web:01`) without detecting or associating the user's Google account email. This breaks consistency with ChatGPT Web (`chatgpt:<email>`) and Claude Web (`claude:web:<email>`), prevents account deduplication on re-login, and leaves the account email column blank in `amux account list`.

## What Changes

- Add Google account email and subscription plan extraction for Gemini Web by inspecting authenticated HTML metadata (`WIZ_global_data`, `aria-label`, and profile card elements from `https://gemini.google.com/app`).
- Update `loginGeminiWeb` in `pkg/ui/login.go` to use `savePoolLogin` with the detected email, naming accounts with a brand-scoped identifier (e.g. `gemini:web:<email_prefix>`) and updating existing entries on re-login.
- Auto-extract ancillary Google cookie `__Secure-1PSIDTS` alongside `__Secure-1PSID` to ensure reliable authenticated page loads and session exchange.
- Provide backfill resolution for existing anonymous `gemini_web` rows so previously saved accounts can discover their identity.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `account-identity`: Gemini Web accounts are identified by email and mapped to stable brand IDs (`gemini:web:<email_prefix>`) matching the "One account per product per email" requirement.
- `web-providers`: Gemini Web login extracts user profile identity (email and plan) from the authenticated session.

## Impact

- `pkg/browser/gemini_account.go`: New module to parse account email and plan from Gemini web HTML and provide `FetchGeminiAccount`.
- `pkg/browser/gemini_account_test.go`: Unit tests for HTML email parsing and plan extraction.
- `pkg/ui/login.go`: Update `loginGeminiWeb` to use `FetchGeminiAccount` and `savePoolLogin`.
- `pkg/cli/id.go`: Sync provider accounts with non-empty `Account` field into identities.
