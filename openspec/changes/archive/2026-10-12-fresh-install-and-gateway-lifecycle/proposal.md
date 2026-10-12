## Why

A fresh install (AMUX_HOME with only accounts.json) migrated the Claude Code (`type: claude`, `enabled: false`) and Codex (`type: codex`) logins as active API-key accounts in the rotation pool, and listed the live Claude login again as "Claude Code (email unknown)". The gateway then spent the Claude Code subscription the user had turned off.

A second gateway start that could not bind :8787 deleted the running gateway's pid files on exit, so `amux status` showed pid 0 and the supervisor could no longer be found.

## What Changes

- Migration reads `enabled: false`, classifies CLI OAuth types (`claude`, `codex`, `antigravity`, …) as subscriptions and keeps real API keys as API keys.
- System-adapter sync skips an unknown-email subscription when that product already has one.
- Gateway and supervisor remove a pid file only while it names their own pid; a second supervisor refuses to start while one is alive.

## Capabilities

### Modified Capabilities
- `account-identity`: fresh-install tiers and off state.
- `gateway-http`: gateway lifecycle pid ownership.

## Impact

- `pkg/identity/migrate.go`, `pkg/cli/id.go`, `pkg/gateway/gateway.go`, `pkg/cli/gateway.go`.
