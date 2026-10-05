## Why

amux could only reach coding agents through env/config hooks for four IDEs (Claude Code, Codex, Cursor, Antigravity), so agents such as Gemini CLI, VS Code Copilot, Windsurf, opencode, Zed or Claude Desktop could not use the user's chat accounts at all. Meta Muse — a capable chat/agent platform with no public API — was not supported. Authentication leaned on the macOS Keychain (blocking prompts, unusable off macOS), several default model ids had been retired upstream (requests 404'd), and test runs leaked into the developer's real `~/.amux`.

## What Changes

- Add an MCP server (`amux mcp`, stdio JSON-RPC, protocol 2025-06-18/2025-03-26/2024-11-05) exposing `amux_ask`, `amux_providers`, `amux_status` and `muse_*` tools, plus `amux mcp install|uninstall|status|config` that registers amux in 10 MCP hosts.
- Add a Meta Muse web provider (`muse_web`, `amux login muse`) written from scratch in Go: a reusable CDP page driver (`pkg/browser`) and a Muse driver (`pkg/muse`) that drive the logged-in web app in a dedicated browser profile. Muse joins the pool (web tier) with tool-call emulation, and is shared with the MCP tools.
- Add a keychain-free secret store: `amux config secret-store file` moves every secret amux uses into an AES-256-GCM vault in `~/.amux`; web cookie refresh then skips keychain-protected browser cookie stores and reads amux's own profiles over CDP; `amux hook --claude` gives Claude Code a placeholder credential so it never needs its keychain login behind the gateway.
- Replace retired model ids with ones verified against each provider's official model page (Anthropic, OpenAI/Codex, Google Gemini, Groq, xAI, Kimi), adapt Claude request parameters per model generation, and retire the GitHub Models login (service shut down 2026-07-30).
- Fix: deterministic `amux <typo>` suggestions; tests can no longer read or write the real `~/.amux` or OS keychain.

## Capabilities

### New Capabilities
- `mcp-server`: amux as a Model Context Protocol server and its installer for MCP hosts.
- `muse-web-provider`: Meta Muse through a browser-driven web session, as a pool backend and as MCP tools.
- `secret-storage`: selectable keychain / encrypted-file secret backend.
- `model-catalog`: verified default model ids and per-generation request rules.

### Modified Capabilities
- `ide-integration`: Claude hook sets a placeholder token; MCP registration added as a second integration path.
- `web-providers`: session refresh honours keychain-free mode.
- `account-identity`: test isolation extends to the secret store.

## Impact

- New packages: `pkg/mcp`, `pkg/muse`; new files `pkg/browser/cdp_page.go`, `pkg/provider/muse_web.go`, `pkg/auth/secretstore.go`, `pkg/tools/claude_models.go`, `pkg/cli/mcp.go`.
- New CLI: `amux mcp …`, `amux login muse`, `amux config secret-store …`; `amux version` reports the real platform.
- Config: `accounts.json` gains `browserProfile` and `cdpEndpoint` (muse_web); new files `~/.amux/secret_store`, `~/.amux/secrets.vault`, `~/.amux/browser-profiles/muse`.
- No new third-party dependencies (CDP over the existing gorilla/websocket).
