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

