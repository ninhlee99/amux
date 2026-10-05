## 1. Remove Muse Module & Deprecate Muse Tooling

- [x] 1.1 Remove `pkg/muse/` directory and all browser driver files
- [x] 1.2 Remove `pkg/provider/muse_web.go` and `pkg/provider/muse_web_test.go`
- [x] 1.3 Remove `RegisterMuseTools`, `MuseClient`, and `muse_*` MCP tools from `pkg/mcp/tools.go`
- [x] 1.4 Clean up `pkg/cli/` (remove muse login subcommand and flags)

## 2. Clean Up Hardcoded Domain Heuristics in Web Providers

- [x] 2.1 Remove `resolveUserTaskIntent` and hardcoded task string injections in `pkg/provider/chatgpt_web.go`
- [x] 2.2 Update `pkg/provider/prompt_test.go` and `pkg/provider/chatgpt_sentinel_test.go` if needed

## 3. Strict & Robust Web-Loop Tool Engine

- [x] 3.1 Refactor `pkg/tools/webloop.go` to remove brittle regex guesses on conversational prose
- [x] 3.2 Ensure explicit tool markup (`<tool_call>`, `[tool_call]`, `<<<AMUX_TOOL>>>`) is parsed with typed schema validation
- [x] 3.3 Verify and update `pkg/tools/` unit tests to ensure high accuracy and zero false positives

## 4. Verification & Validation

- [x] 4.1 Run `go test ./...` and ensure 100% of unit and integration tests pass
- [x] 4.2 Run `openspec validate --strict` to ensure spec compliance
