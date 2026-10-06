## 1. WebLoop & Prompt Delta Fixes

- [x] 1.1 Fix multi-turn tool delta handling in `pkg/provider/prompt.go` so trailing `role="tool"` results are correctly extracted with task goals.
- [x] 1.2 Enable seamless conversation reuse for web adapters in `pkg/provider/claude_web.go`, `chatgpt_web.go`, and `gemini_web.go` to eliminate per-turn chat recreation.
- [x] 1.3 Enhance `StripWebToolMarkup` and lenient regex parsing in `pkg/tools/webloop.go` to prevent any prompt delimiter or thinking leakage.

## 2. OpenAI Bridge Streaming Tool Fix

- [x] 2.1 Refactor streaming in `pkg/bridge/openai.go` to buffer potential tool call syntax and prevent premature plaintext emission of raw markup to Cursor/Windsurf.
- [x] 2.2 Verify structured `tool_calls` delta generation for Cursor and other OpenAI-compatible IDE clients.

## 3. MCP Server & Web Accounts Stability

- [x] 3.1 Update `pkg/mcp/backend.go` and `pkg/mcp/tools.go` to sanitize web markup in one-shot MCP queries and add resilient retries.
- [x] 3.2 Add Cline and Roo Code support to `pkg/mcp/install.go` and ensure reliable detection.

## 4. IDE Detection and Pre-flight Automation

- [x] 4.1 Update `pkg/cli/doctor.go` to inspect `/Applications` for macOS bundles and check availability for `aider` and `opencode`.
- [x] 4.2 Add `--setup` flag support to `amux run cursor` and `amux run windsurf` to automate IDE settings configuration.
- [x] 4.3 Enhance gateway error messages for 503 / 429 with actionable remediation commands.

## 5. Verification and Validation

- [x] 5.1 Add unit tests for tool delta prompt building, markup stripping, and OpenAI streaming buffering.
- [x] 5.2 Validate OpenSpec with `openspec validate --strict` and run all tests with `go test ./...`.
