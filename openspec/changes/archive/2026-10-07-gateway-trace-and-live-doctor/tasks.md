## 1. Request tracing
- [x] 1.1 Assign / validate `X-Amux-Request-Id` at the gateway entry and echo it on the response
- [x] 1.2 Strip it from outbound upstream headers
- [x] 1.3 Record it in `[req]` log lines, request entries and `errors.log`
- [x] 1.4 Append it to client-facing error messages for every dialect
- [x] 1.5 Tests: id echoed, not leaked upstream, named in error body

## 2. Live doctor
- [x] 2.1 `amux doctor --live [--provider <id>]`: gateway, Anthropic, OpenAI SSE, tool roundtrip
- [x] 2.2 Fix suggestions per failure class (auth, rate limit, no accounts, prose instead of tool)
- [x] 2.3 Tests against a fake gateway; help text

## 3. Health-aware routing
- [x] 3.1 Try degraded accounts last within a tier, keep round-robin among equals
- [x] 3.2 Remove recursive RLock in `GetAllReports`
- [x] 3.3 Test: degraded account is not served while a healthy one works

## 4. Web tool parser
- [x] 4.1 `FuzzParseWebTools` (catalog names only, object inputs, AMUX blocks stripped)
- [x] 4.2 Schema-aware object inputs for bare values; drop unrunnable calls
- [x] 4.3 Quote bare keys in Gemini native calls; tighten its test

## 5. Hygiene
- [x] 5.1 `gateway.log` created / tightened to 0600
