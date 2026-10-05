## 1. Pool

- [x] 1.1 Subscriptions out of the pool by default (`Identity.CanAutoRotate`), `PoolMemberFilter`, `ProfileInPool`, `ProfileThreshold`
- [x] 1.2 Router admits subscription adapters only via the pool filter
- [x] 1.3 Claude rotator switches only between pooled profiles; per-account threshold
- [x] 1.4 Migration v2 clears the legacy subscription "disabled" flag; stop forcing it
- [x] 1.5 `amux pool` command

## 2. Switching without re-login

- [x] 2.1 `amux switch` restores bundles (via gateway when running), no stale credential overwrite
- [x] 2.2 Rotator snapshot guards against a keychain holding another account; follow external logins
- [x] 2.3 `.claude.json`: switch only `oauthAccount`
- [x] 2.4 `amux login claude` writes the OS-user keychain item

## 3. Clean removal

- [x] 3.1 Remove daemon auto-hook monitor
- [x] 3.2 Exact unhook (AGY, Codex, shell rc, Claude env), `amux off`, stop warning
- [x] 3.3 `mcp uninstall` everywhere registered, remove backups / amux-created files
- [x] 3.4 `uninstall` rewrite; `--purge` removes master key

## 4. Docs and tests

- [x] 4.1 Tests: router, rotator, identity, profile merge, gateway unhook, MCP uninstall, CLI
- [x] 4.2 Help, completion, README, codebase map
