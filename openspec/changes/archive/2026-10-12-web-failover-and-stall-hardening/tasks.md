## 1. Routing

- [x] 1.1 `startStream` primes streams; used by preferred, tier and SendNamed paths
- [x] 1.2 Tests: in-stream limit fails over and cools down; mid-stream error after content passes through

## 2. Providers

- [x] 2.1 Gemini stall watch + retry in new chat; canned-error detection with held deltas
- [x] 2.2 Claude Web `resetsAt` / Retry-After and in-stream rate_limit errors as `RateLimitError`
- [x] 2.3 ChatGPT Web Retry-After as `RateLimitError`

- [x] 2.4 ChatGPT expired-token refresh before / at sentinel
- [x] 2.5 `checkSessionAccount` on ChatGPT / Claude / Gemini browser refresh; Gemini persists only after the check
- [x] 2.6 Gemini stall watch covers the header wait

## 3. Identity

- [x] 3.1 Web type before plan in migration; `repairWebTiers` on load

- [x] 3.2 Direct-credential fallback gated on pooled subscription, never for pinned requests

## 4. Prompts

- [x] 4.1 `SimplifyHarnessToolResult`, `SimplifyTaskNotifications`
- [x] 4.2 Background-agent cue; Claude relay intro

## 5. Verification

- [x] 5.1 `go test ./...`
- [x] 5.2 Live: `scripts/test_provider.sh` and tool/skill/MCP/subagent matrix on Claude Web, ChatGPT Web, Gemini Web
