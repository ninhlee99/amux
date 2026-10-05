## Why

Users with several IDE subscriptions need two guarantees: amux never moves them onto another subscription unless they asked for it, and anything amux adds can be removed without side effects. Several paths broke this: subscriptions defaulted into automatic rotation, the gateway daemon re-hooked/unhooked IDEs and rewrote the keychain on its own, `amux switch` with the gateway running saved the new account's token into the old account's bundle (forcing a re-login), switching rewrote all of `~/.claude.json`, and uninstall deleted user-owned settings while leaving MCP entries behind.

## What Changes

- Rotation pool: subscriptions are out of the pool until `amux pool add <id>`; automatic switching only moves from a pooled account to another pooled account (router, Claude rotator, MCP). Web/API accounts stay pooled by default. Per-account thresholds now drive the Claude rotator.
- **BREAKING**: subscriptions that were implicitly rotating stop doing so until added to the pool. The legacy migration flag that marked every subscription "disabled" is cleared once (schema v2).
- Removed the daemon's automatic hook/unhook + keychain rotation monitor; hooks change only on explicit commands.
- `amux switch`: restores the saved bundle (through the gateway when it runs), never writes stale identity credentials over it; the rotator never snapshots a keychain holding another account; `~/.claude.json` switches only `oauthAccount`; `amux login claude` writes the keychain item Claude Code reads.
- Clean removal: new `amux off`; `unhook` removes only what `hook` added (AGY `modelProvider` only if amux set it, no rc file creation, symlinks/permissions kept, Codex files amux created are deleted, no global `OPENAI_BASE_URL`); `mcp uninstall` removes from every registered host plus backups; `uninstall` stops the gateway, unhooks, removes MCP/slash commands/session hooks, removes only binaries that are amux, and `--purge` deletes amux's master key.
- CLI: `amux pool`, `amux off`, simpler help and account table (TYPE/ACTIVE/POOL), README rewritten.

## Capabilities

### New Capabilities

### Modified Capabilities
- `subscription-rotation`: pool-only automatic switching; no automatic hooking.
- `provider-pool-routing`: subscriptions require manual pool membership.
- `ide-integration`: exact unhook, `amux off`, clean uninstall.
- `mcp-server`: subscription selection follows the pool; uninstall cleans up.
- `account-identity`: switching without re-login.

## Impact

`pkg/identity`, `pkg/router/pool.go`, `pkg/proxy/{rotator,server,client}.go`, `pkg/profile/manager.go`, `pkg/gateway/{hook,gateway,hookstate}.go`, `pkg/hook/hook.go`, `pkg/mcp/{backend,install}.go`, `pkg/auth/{crypto.go,oauth/*}`, `pkg/cli/*`, README.
