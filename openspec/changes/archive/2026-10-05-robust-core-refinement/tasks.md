## 1. Web-Loop Deterministic Tool Calling

- [x] 1.1 Audit `pkg/tools/webloop.go` to ensure tool extraction only processes structured tool markup
- [x] 1.2 Verify `FinalizeWebToolCalls` cleanly returns assistant prose when no tool calls are present
- [x] 1.3 Verify `pkg/tools/` tests for webloop extraction and refusal handling

## 2. IDE Integration & Teardown Guarantees

- [x] 2.1 Verify `amux stop`, `amux off`, and `amux hook --unhook` cleanly reset environment and config files
- [x] 2.2 Verify `amux mcp install` correctly configures all supported IDE targets
- [x] 2.3 Verify `pkg/gateway/` and `pkg/mcp/` unit tests

## 3. Comprehensive Verification

- [x] 3.1 Run `go test ./...` across all packages
- [x] 3.2 Run `openspec validate --all --strict`
