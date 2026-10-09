# gateway-http Specification

## Purpose
amux runs a local Universal AI Gateway (default `127.0.0.1:8787`) that speaks the wire formats coding agents already use — Anthropic Messages, OpenAI Chat Completions / Responses, and Google Gemini — so Claude Code, Codex, Cursor, Antigravity and any OpenAI-compatible tool can be pointed at it without changing the tool.

## Requirements

### Requirement: Multi-dialect endpoints
The gateway SHALL serve `POST /v1/messages` (Anthropic), `POST /v1/chat/completions` and `POST /v1/responses` (OpenAI), `POST /v1beta/models/{model}:generateContent|streamGenerateContent|countTokens` (Gemini) and `GET /v1/models`, translating every request into the canonical `types.ChatRequest` and every reply back into the caller's dialect, including streaming (SSE).

#### Scenario: Claude Code request is answered in Anthropic format
- **WHEN** Claude Code sends `POST /v1/messages` with `stream: true`
- **THEN** the gateway streams Anthropic SSE events (`message_start`, `content_block_*`, `message_delta`, `message_stop`) regardless of which backend served it

#### Scenario: Model list follows the caller's dialect
- **WHEN** `GET /v1/models` carries an `anthropic-version` header
- **THEN** the gateway returns the Anthropic model-list shape, otherwise the OpenAI shape

### Requirement: Pinning a provider per request
The gateway SHALL accept `X-Provider` with either an exact pool account id or an account family. An exact id (e.g. `gemini:web:01`) MUST route to that account only — even when it is out of the automatic rotation — with no failover, and MUST be refused for accounts the user disabled. A family (`gemini:web`, `gemini:web:*`, `chatgpt`; case-insensitive, matched on whole `:`-separated segments) SHALL let amux choose among that family's accounts with the pool's affinity, cooldown, quarantine and failover rules, never leaving the family.

#### Scenario: Explicit pin
- **WHEN** a request has `X-Provider: gemini:web:01`
- **THEN** only `gemini:web:01` serves it and no failover to another account happens

#### Scenario: Family pick with failover
- **WHEN** a request has `X-Provider: gemini:web`, `gemini:web:01` is rate-limited and `gemini:web:02` is healthy
- **THEN** `gemini:web:02` serves it and no account outside `gemini:web` is tried

#### Scenario: Unknown family
- **WHEN** a request has `X-Provider: gemini:w`
- **THEN** the request fails with "matches no account" instead of routing elsewhere

### Requirement: Admin control plane
The gateway SHALL expose its own JSON administration endpoints only under `/_am/` (status, guard, switch); it MUST NOT add `/_am/*` routes that mirror client slash commands.

#### Scenario: Status endpoint
- **WHEN** `GET /_am/status` is called on loopback
- **THEN** the gateway returns pool, rotator and guard state as JSON

### Requirement: Loopback by default, token on public bind
The gateway SHALL bind to loopback by default. When started public (`amux start -p`, `0.0.0.0`) it MUST require a bearer token of the form `amux-<hex>` (issued fresh per public start, stored `0600` in `~/.amux/proxy.token`) and rate-limit failed authentication per client IP.

#### Scenario: Public request without token
- **WHEN** a non-loopback client calls the public gateway without the token
- **THEN** the gateway answers 401 and records the failure for brute-force limiting

### Requirement: No hidden spend from the host's own keys
While the gateway is up and no pool account can serve a request, it MUST refuse the request rather than fall back to the host process's own `ANTHROPIC_API_KEY`. When refusing on `/v1/messages` due to pool exhaustion, the gateway SHALL return HTTP 503 formatted as a valid Anthropic API JSON error (`{"type":"error","error":{"type":"permission_error","message":"..."}}`) so client coding tools (such as Claude Code) surface actionable guidance instead of plain-text errors.

#### Scenario: Pool exhausted
- **WHEN** every pool account is cooling down and the caller brought no credential
- **THEN** the gateway returns an error instead of using an environment API key

#### Scenario: Pool exhausted returns structured Anthropic error
- **WHEN** every pool account is cooling down or unavailable and Claude Code calls `POST /v1/messages`
- **THEN** the gateway returns HTTP 503 with an Anthropic JSON error structure containing the failure explanation

### Requirement: Path-based and query-based provider routing
The gateway SHALL support provider selection via URL path prefixes (`/p/{provider}/...` or `/provider/{provider}/...`) and query parameters (`?provider={provider}`) in addition to the `X-Provider` HTTP header. The path prefix SHALL be stripped before routing to standard endpoint handlers (`/v1/messages`, `/v1/chat/completions`, etc.).

#### Scenario: Path-based provider routing
- **WHEN** a client sends a request to `POST http://127.0.0.1:8787/p/gemini:web:01/v1/messages`
- **THEN** the gateway routes the request specifically to `gemini:web:01` without requiring custom HTTP headers

#### Scenario: Query-based provider routing
- **WHEN** a client sends a request with `POST http://127.0.0.1:8787/v1/chat/completions?provider=chatgpt`
- **THEN** the gateway routes the request specifically to the `chatgpt` family

### Requirement: Detached daemon logging
The background gateway daemon SHALL redirect standard output and standard error to `~/.amux/gateway.log` with file mode `0600`.

#### Scenario: Daemon logging verification
- **WHEN** `amux start` launches the gateway daemon
- **THEN** startup events and background errors are recorded in `~/.amux/gateway.log`

### Requirement: Request trace id
The gateway SHALL give every request an id in `X-Amux-Request-Id`, keeping a caller-supplied id only when it is at most 64 characters of `[A-Za-z0-9_-]`. The id MUST be returned on the response, written on the request's log line, request entry and `errors.log` block, appended to client-facing error messages, and MUST NOT be forwarded to any upstream.

#### Scenario: Failed turn names its id
- **WHEN** a `/v1/chat/completions` request fails because every pool account errored
- **THEN** the error message ends with `[amux request <id>]` and `errors.log` has a block with `req=<id>`

#### Scenario: Id stays local
- **WHEN** Claude Code is reverse-proxied to Anthropic
- **THEN** the client receives `X-Amux-Request-Id` and the Anthropic request carries no such header

### Requirement: Live end-to-end check
`amux doctor --live` SHALL send real turns through the running gateway — an Anthropic Messages turn, a streamed OpenAI Chat Completions turn and a Bash tool roundtrip — optionally pinned with `--provider <id|family>`, and print for each check its result, request id and suggested fix commands. It MUST stop after the gateway probe when the gateway is not answering.

#### Scenario: Web account answers in prose
- **WHEN** the serving account replies to the tool roundtrip with text and no `tool_use`
- **THEN** that check fails and suggests retrying per account with `--provider`

#### Scenario: Gateway down
- **WHEN** nothing answers at the gateway address
- **THEN** only the gateway check is shown, failed, with `amux start` as the fix

### Requirement: Prometheus metrics endpoint
The gateway SHALL expose `GET /_am/metrics` and `GET /metrics` formatted according to Prometheus text exposition format version 0.0.4. The metrics MUST include total request counts partitioned by client dialect and status (`amux_requests_total`), token usage (`amux_tokens_total`), active sessions (`amux_active_sessions`), and account health gauges (`amux_account_health`).

#### Scenario: Prometheus scraper accesses metrics
- **WHEN** a client performs `GET http://127.0.0.1:8787/_am/metrics`
- **THEN** the gateway returns HTTP 200 with `Content-Type: text/plain; version=0.0.4` containing Prometheus metric lines

#### Scenario: Metrics endpoint via alias /metrics
- **WHEN** a scraper requests `GET http://127.0.0.1:8787/metrics`
- **THEN** the gateway returns the same metrics exposition output

### Requirement: Privacy-safe audit logging
The gateway SHALL record completed turns into `~/.amux/audit.log` with file permissions `0600`. Each log entry MUST be a JSON object containing request id, timestamp, client, provider, account, token counts, latency, and status. It MUST NOT record raw prompt text or model completions.

#### Scenario: Turn completes successfully
- **WHEN** a turn finishes via the gateway
- **THEN** an entry is appended to `~/.amux/audit.log` containing request metadata without prompts

### Requirement: Retryable status for fast routing failures
For a streaming Anthropic Messages request the gateway SHALL open the SSE stream only once routing has taken 5 seconds (or a keepalive is due), so a routing failure within that time is answered with an HTTP error status instead of a stream that ends in an error event. An unreachable upstream or its cooldown SHALL be answered with `503` and a `Retry-After` matching the back-off (10 seconds, 30 for a Cloudflare challenge); error messages SHALL name the reason and remaining cooldown and MUST NOT contain upstream HTML.

#### Scenario: Offline Claude Code turn
- **WHEN** Claude Code sends a streaming turn while the only pool account is unreachable
- **THEN** it receives HTTP 503 with `Retry-After` and no `message_start`, and retries the turn itself

### Requirement: Email redaction keeps file names and no-reply addresses
Outbound privacy redaction SHALL NOT replace an email-like match whose final domain label is a file extension (for example `page@3f9a1c2e.webm`, `logo@2x.png`), or a no-reply address (`noreply@…`, `no-reply@…`, `…@users.noreply.github.com`). Other email addresses SHALL still be replaced with the sample.

#### Scenario: Playwright capture file name
- **WHEN** a tool result contains `mv /tmp/out/page@3f9a1c2e.webm dashboard.webm`
- **THEN** the text sent upstream is unchanged
