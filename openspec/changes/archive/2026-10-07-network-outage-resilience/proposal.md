## Why

When the network dropped or the public IP changed, chatgpt.com answered with a Cloudflare challenge page (HTTP 403). amux put the whole HTML page into the error. Random tokens in it ("…429…", "tpm") matched the rate-limit check, so the only web account cooled down for 2 minutes. A later 403 was cut at 2KB before the Cloudflare marker and counted as an auth failure: two of those quarantined the account for 60 minutes. Meanwhile Claude Code got 200 + `message_start` and then an error event, showed "Part of the response never arrived" and did not retry, so the user saw `502 … cooling down` on every turn long after the network came back.

The session-scoped thread key (2026-10-07-web-thread-per-session) also hashed Claude Code's per-turn system notes (`<total_tokens>`, environment), so every turn opened a new ChatGPT conversation with the full transcript.

## What Changes

- A new `types.ErrUpstreamUnreachable` covers DNS / no-route / refused / reset transport failures and Cloudflare challenge pages (`Cf-Mitigated`, Cloudflare HTML 403/503, or challenge markers anywhere in the page). Web error bodies are summarized to one line and never carry the HTML.
- The health tracker records unreachable errors without touching score, auth streak or quarantine. A plain 401/403 stays an auth failure; timeouts stay server errors.
- The router cools an unreachable account down for 10s, or 30s for a Cloudflare challenge (not 2 minutes) and its errors say why and for how long: `cooling down (network unreachable, retry in 8s)`.
- The Claude bridge opens the SSE stream only after routing takes 5s (or on the first keepalive). A faster failure is a real HTTP error; unreachable / its cooldown is `503` with a matching `Retry-After`, which Claude Code retries with backoff.
- The web thread key hashes only the leading system turns.

## Impact

- `pkg/types/chat.go`, new `pkg/provider/upstream_err.go`, `pkg/provider/chatgpt_web.go`, `pkg/provider/chatgpt_sentinel.go`.
- `pkg/guard/health.go`, `pkg/router/pool.go`.
- `pkg/bridge/claude.go`, `pkg/bridge/headers.go`.
- `pkg/provider/project_conv.go` (`ThreadKey`).
