# Design: Production Observability & Auth Health

## Context

AMUX functions as a universal gateway between coding tools (Claude Code, Cursor, Codex, AGY) and upstream providers (Anthropic, OpenAI, Gemini, Web sessions). As usage scales, operator visibility into gateway throughput and credential lifetimes becomes critical.

## Architecture Decisions

### 1. Zero-dependency Prometheus Metrics
Instead of pulling heavy third-party Prometheus client libraries that bloat the binary or complicate cross-compilation, AMUX implements an in-tree collector in `pkg/metrics` using atomic counters and concurrent maps. It renders standard Prometheus text format (`# HELP`, `# TYPE`, key-value labels).
Exposed on `GET /_am/metrics` and `GET /metrics`.

Key metrics:
- `amux_requests_total{client="...", provider="...", status="ok|err"}` (Counter)
- `amux_request_duration_ms{client="...", provider="..."}` (Gauge / rolling avg)
- `amux_tokens_total{account="...", direction="in|out"}` (Counter)
- `amux_account_health{account="...", provider="..."}` (Gauge: 0-100)
- `amux_active_sessions` (Gauge)

### 2. Privacy-Safe Audit Logging
When a turn concludes in `pkg/bridge/requestlog.go`, an audit event is appended to `~/.amux/audit.log` (mode `0600`).
Event schema:
```json
{
  "timestamp": "2026-10-07T14:40:00Z",
  "request_id": "ax_...",
  "client": "claude-code",
  "dialect": "claude",
  "account": "claude:01",
  "provider": "anthropic",
  "model": "claude-3-7-sonnet",
  "tokens_in": 1200,
  "tokens_out": 350,
  "duration_ms": 1420,
  "status": "ok",
  "error": ""
}
```
No user prompt text or completions are recorded.

### 3. Credential Expiry & Health Diagnostics
`pkg/auth/oauth/jwt.go` is augmented with `ParseJWTExpiry(jwtToken string) (time.Time, bool)`.
`pkg/auth/health.go` defines `InspectCredentialHealth` which checks:
- OAuth tokens for Claude, OpenAI Codex, Antigravity by parsing JWT claims or stored reset timestamps.
- Web cookies for expiration or missing fields.
- API keys format and presence.

The command `amux doctor auth` renders a clear overview table showing validity and days remaining before expiration.

### 4. Explicit Provider Capabilities
`pkg/types/capability.go` defines:
```go
type ProviderCapability struct {
    Streaming   bool `json:"streaming"`
    NativeTools bool `json:"native_tools"`
    WebTools    bool `json:"web_tools"`
    Vision      bool `json:"vision"`
    Reasoning   bool `json:"reasoning"`
}
```
Adapters can declare their capabilities explicitly to simplify capability matching.
