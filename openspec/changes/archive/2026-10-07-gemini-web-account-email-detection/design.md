## Context

AMUX supports web login across ChatGPT, Claude, and Gemini. While ChatGPT and Claude expose clear REST endpoints (`/api/auth/session` and `/api/account`) providing the authenticated user's email, Gemini relies on Google Accounts. Gemini Web login previously only captured `__Secure-1PSID` and omitted email detection, causing Gemini Web accounts to be saved anonymously with numeric IDs (`gemini:web:01`).

## Goals / Non-Goals

**Goals:**
- Extract the Google account email and subscription tier (free vs pro/Advanced) when logging in with Gemini Web.
- Integrate Gemini Web into `savePoolLogin`, assigning named IDs (`gemini:web:<email_prefix>`).
- Support re-login deduplication (updating existing account rows rather than duplicating).
- Ensure automatic cookie jar assembly includes `__Secure-1PSIDTS` when available from local browsers or CDP.

**Non-Goals:**
- Calling private Google OAuth APIs or requiring Google Cloud API keys.
- Changing the underlying chat RPC protocol (`BardFrontendService/StreamGenerate`).

## Decisions

1. **Extract email from server-rendered HTML:**
   - Query `GET https://gemini.google.com/app` with the authenticated cookie header.
   - Inspect WIZ global data (`"oPEP7c": "<email>"`), account anchor `aria-label="... (<email>)"`, and profile card elements.
   - Fall back gracefully without failing login if offline or if Google alters HTML structure.

2. **Standardize on `savePoolLogin`:**
   - Migrate `loginGeminiWeb` from manual `nextPoolID()` to `savePoolLogin("gemini_web", accountEmail, ...)`.
   - Maps to `types.AccountNamedID("gemini", "web", email)` producing `gemini:web:<email_prefix>`.

3. **Multi-cookie support:**
   - Extract both `__Secure-1PSID` and `__Secure-1PSIDTS` when auto-extracting from browsers to satisfy Google's session verification.

## Risks / Trade-offs

- [Risk: Google changes WIZ property name `oPEP7c`] → Mitigation: Multi-layered regex fallbacks checking `aria-label`, profile card text, and general email regex excluding static Google domains.
- [Risk: No email found when network fails] → Mitigation: Retain fallback to anonymous pool slot without blocking login.
