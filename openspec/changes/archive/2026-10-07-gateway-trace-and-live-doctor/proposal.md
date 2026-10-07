## Why

When an IDE turn fails behind amux, the user sees "AI stopped answering" with no way to tell which layer broke (gateway, router, account session, stream, or web tool parser). Gateway log lines from one request could not be tied together, `amux doctor` only checked config, and the router kept giving degraded accounts their round-robin turn. Fuzzing the web tool parser also showed it could hand IDEs tool inputs that are not JSON objects, which Claude Code rejects.

## What Changes

- Every gateway request gets an `X-Amux-Request-Id` (kept if the caller sent a safe one). It is returned on the response, written on the `[req]` log line, `requests` entries and `errors.log` blocks, appended to client-facing error messages, and never sent upstream.
- `amux doctor --live [--provider <id>]` sends real turns through the running gateway (Anthropic JSON, OpenAI SSE stream, Bash tool roundtrip) and prints each result with its request id and the commands most likely to fix a failure.
- Within a tier, the router tries degraded accounts (health score < 80) after healthy ones; equally healthy accounts keep round-robin order.
- Web tool calls always reach the IDE with a JSON-object input: a bare value maps onto the tool's single required string parameter, an unrunnable call is dropped, and Gemini's native `call:…{Key: "v"}` syntax keeps its keys.
- `gateway.log` is owner-only (0600) like `errors.log`; `HealthTracker.GetAllReports` no longer takes a recursive read lock.

## Impact

- `pkg/types/requestid.go`, `pkg/proxy/server.go`, `pkg/guard/sanitizer.go`, `pkg/bridge/requestlog.go` and the bridge error writers, `pkg/monitor/error_diag.go`, `pkg/telemetry/logger.go`.
- `pkg/cli/doctor.go`, `pkg/cli/doctor_live.go`, `pkg/cli/help.go`.
- `pkg/router/pool.go`, `pkg/guard/health.go`.
- `pkg/tools/webloop.go`, new `FuzzParseWebTools`.
