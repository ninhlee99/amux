## 1. Web Tool Loop & Fuzzy JSON Normalization

- [x] 1.1 Enhance fuzzy JSON repair in `pkg/tools/webloop.go` to handle unescaped quotes in shell arguments
- [x] 1.2 Add schema normalization for bash, edit, and read tool variants
- [x] 1.3 Add unit tests in `pkg/tools/webloop_fuzzy_test.go` verifying edge case JSON extraction

## 2. MCP 2.0 Extended Tools & Context

- [x] 2.1 Add workspace `context` parameter support to `amux_ask` in `pkg/mcp/tools.go`
- [x] 2.2 Implement `amux_review` tool in `pkg/mcp/tools.go` for multi-file git diff code analysis
- [x] 2.3 Add unit tests in `pkg/mcp/server_test.go` verifying `amux_review` and context handling in `amux_ask`

## 3. Gateway Fail-Safe & Pass-Through

- [x] 3.1 Implement transparent fail-safe forwarding in `pkg/proxy/passthrough.go` when pool router reports complete exhaustion
- [x] 3.2 Wire fail-safe handler into `/v1/messages` bridge
- [x] 3.3 Add unit tests verifying direct fail-safe invocation on router failure

## 4. Verification & Validation

- [x] 4.1 Run full test suite `go test ./...`
- [x] 4.2 Validate change with `openspec validate --strict`
