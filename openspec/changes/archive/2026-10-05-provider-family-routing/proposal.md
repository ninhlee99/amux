## Why

Users want to say "use Gemini web" (or ChatGPT, or Muse) without naming one account, and let amux pick a healthy account of that family with failover. `X-Provider` only accepted an exact id. Investigating this also showed that the router never applied its manual-only filter, so `AUTO-SWITCH: OFF` accounts could be auto-selected.

## What Changes

- `X-Provider` (gateway) and `amux_ask.provider` (MCP) accept an account family such as `gemini:web`, `gemini:web:*` or `chatgpt`; amux selects and fails over among that family only. An exact id still pins one account.
- New `AccountPoolRouter.SendProvider` / `FamilyMembers`; `Send` and family selection share one selection core.
- Fix: automatic selection skips manual-only accounts; an exact pin still reaches them.

## Capabilities

### New Capabilities

### Modified Capabilities
- `gateway-http`: pinning accepts account families.
- `mcp-server`: `amux_ask.provider` accepts families.
- `provider-pool-routing`: manual-only rule now enforced and covers family selection.

## Impact

`pkg/router/pool.go`, `pkg/bridge/headers.go`, `pkg/mcp/backend.go`, `pkg/mcp/tools.go`; tests in `pkg/router/send_provider_test.go`, `pkg/bridge/provider_family_test.go`.
