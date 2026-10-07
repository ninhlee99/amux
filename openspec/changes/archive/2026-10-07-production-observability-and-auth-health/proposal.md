## Why

Production AI gateways require standards-based observability, privacy-safe audit trails, proactive credential health monitoring, and explicit provider capability contracts to operate reliably:

1. **Standards-based Metrics**: While AMUX has an internal `/_am/status` JSON endpoint, it lacks a Prometheus-compatible metrics endpoint (`GET /_am/metrics` and `GET /metrics`) for real-time monitoring and alerting.
2. **Privacy-Safe Audit Trail**: Gateway operators need an append-only structured audit log of request metadata (timestamp, request id, client, account, tokens, duration, success/failure) stored at `0600` permissions without recording sensitive prompts.
3. **Proactive Credential Expiry Diagnostics**: Credentials (OAuth tokens for Claude/Codex, web cookies, API keys) can expire silently. Users should be able to run `amux doctor auth` to see the exact expiration status and remaining lifetime of their credentials.
4. **Provider Capability Model**: Providers should explicitly declare capabilities (Streaming, NativeTools, WebTools, Vision, Reasoning) via a unified contract rather than scattered ad-hoc string checks.

## What Changes

- Add `pkg/metrics` collector and expose `GET /_am/metrics` (and `GET /metrics`) in standard Prometheus text format (`amux_requests_total`, `amux_request_duration_ms`, `amux_tokens_total`, `amux_account_health`, `amux_active_sessions`).
- Add `pkg/telemetry/audit.go` to append privacy-safe JSONL entries to `~/.amux/audit.log` (mode `0600`) upon every completed turn.
- Add `ParseJWTExpiry` to `pkg/auth/oauth/jwt.go` and add `InspectCredentialHealth` in `pkg/auth/` to evaluate remaining token lifetime and session viability.
- Add `amux doctor auth` (and `amux auth status`) CLI command to display structured credential health.
- Add `pkg/types/capability.go` defining standard provider capabilities.

## Impact

- `pkg/metrics/`: New metrics registry and HTTP Prometheus text formatter.
- `pkg/proxy/server.go`: Expose `/_am/metrics` and `/metrics`.
- `pkg/telemetry/audit.go`: Append audit log entries with safe file permissions.
- `pkg/bridge/requestlog.go`: Call audit logger on turn completion.
- `pkg/auth/oauth/jwt.go`: JWT expiration claim extraction.
- `pkg/auth/health.go`: Credential health inspection.
- `pkg/cli/doctor.go` & `pkg/cli/help.go`: `amux doctor auth` subcommand.
- `pkg/types/capability.go`: Provider capability declarations.
