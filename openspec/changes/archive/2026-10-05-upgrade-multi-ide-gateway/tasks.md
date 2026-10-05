## 1. Foundations and fixes

- [x] 1.1 Make `amux <typo>` suggestions deterministic (sorted candidates, shortest prefix wins)
- [x] 1.2 Sandbox `types.BaseDir()` inside test binaries; fix the identity test that set the unused `AMUX_DIR`
- [x] 1.3 Clean the test artefacts previously leaked into the real `~/.amux`

## 2. CDP page driver

- [x] 2.1 `browser.OpenSession` (launch with `--remote-debugging-port=0`, attach via `DevToolsActivePort` or explicit endpoint, kill only owned browsers)
- [x] 2.2 `browser.Page` (concurrent CDP calls, `Eval`, `InsertText`, `PressKey`, `SetFileInputFiles`, `Navigate`)
- [x] 2.3 Tests against a fake DevTools websocket

## 3. Meta Muse

- [x] 3.1 `pkg/muse` driver: status, login, new/open/list/read chats, chat with stable streaming, attachments, media download, DOM dump, close
- [x] 3.2 `muse_web` provider adapter with thread reuse, synchronous early failures, error classification and tool-call emulation
- [x] 3.3 Wire `muse_web` into accounts.json schema, ids (`muse:web:NN`), priorities and `amux login muse`
- [x] 3.4 Unit tests with a fake page and a fake driver
- [x] 3.5 End-to-end test on real headless Chrome against a page mimicking Muse's DOM (`go test -tags e2e ./pkg/muse`), plus `amux mcp` → `muse_chat` over stdio

## 4. MCP server

- [x] 4.1 JSON-RPC stdio server (initialize/version negotiation, ping, tools/list, tools/call, cancellation, progress)
- [x] 4.2 `amux_*` tools backed by the pool (subscriptions excluded from auto selection) and `muse_*` tools
- [x] 4.3 Installer for Claude Code, Claude Desktop, Cursor, Windsurf, VS Code, Gemini CLI, Antigravity, Codex, opencode, Zed (atomic, backup, JSONC-safe, idempotent)
- [x] 4.4 `amux mcp` CLI, help, completions; smoke test of the built binary over stdio

## 5. Keychain-free authentication

- [x] 5.1 Secret-store seam under `KCGet/KCSet/KCAccount` with encrypted file vault and Claude credentials mirror
- [x] 5.2 Master key never touches the keyring in file mode; `SetSecretStore` migrates key and known items
- [x] 5.3 Cookie refresh skips keychain-protected browser stores and reads amux profiles over CDP
- [x] 5.4 `amux hook --claude` adds the `am-proxy` placeholder token (removed on unhook)
- [x] 5.5 `amux config secret-store [file|keychain]`

## 6. Model catalog

- [x] 6.1 Verify ids on official model pages; update defaults and advertised lists for every provider
- [x] 6.2 Per-generation Claude request rules (thinking, sampling, tool_choice) with tests
- [x] 6.3 Retire the GitHub Models login; regression test against retired ids

## 7. Specs and docs

- [x] 7.1 Baseline OpenSpec specs for existing capabilities
- [x] 7.2 Delta specs for this change; validate strictly
- [x] 7.3 Update README / codebase map for MCP, Muse and secret store
