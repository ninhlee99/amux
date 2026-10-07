## ADDED Requirements

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
