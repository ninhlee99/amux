## 1. Migration
- [x] 1.1 `enabled` field, `isSubscriptionType`, API key first; test
- [x] 1.2 `hasSubscriptionFor` in system-adapter sync; test

## 2. Lifecycle
- [x] 2.1 `RemovePIDFileIfOwn`, `LivePIDInFile`, single supervisor; test

## 3. Verification
- [x] 3.1 `go test ./...`
- [x] 3.2 Live: fresh AMUX_HOME lists the same 7 accounts as the real store and never uses the off Claude Code login; second supervisor refused, status/stop keep the running gateway
