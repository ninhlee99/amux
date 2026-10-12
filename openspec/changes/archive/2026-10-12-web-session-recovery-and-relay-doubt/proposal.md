## Why

After the account check landed, `chatgpt:bi117ute` and `gemini:web:bi117ute` failed with "browser session belongs to ninhle21199": their stored sessions had expired and the browser refresh only looked at the most recently used profile. Edge also held a live bi117ute session in another profile, so the accounts were recoverable without a new login.

claude.ai also occasionally (1 run in 7) replied that it could not verify the relayed tool results and stopped. The existing refusal detector missed it, and its nudge text would only add pressure.

## What Changes

- `browser.ExtractCookies` / `ExtractCookieFromStore` list sessions in every profile; ChatGPT, Claude and Gemini refresh pick the profile signed in to the slot's account (companion cookies from the same profile).
- `tools.IsRelayDoubt` detects relay-doubt replies; adapters reset the thread on it; `sendWithWebNudge` re-sends the unchanged request once on the fresh thread and returns an error (router fails over) if the model declines again.

## Capabilities

### Modified Capabilities
- `web-providers`: per-account browser session recovery.
- `web-tool-optimization`: relay-doubt handling.

## Impact

- `pkg/browser/cookies.go`, `pkg/provider/session_account.go`, `pkg/provider/{chatgpt,claude,gemini}_web.go`, `pkg/provider/web_nudge.go`, `pkg/tools/harness.go`, `scripts/test_provider.sh`.
