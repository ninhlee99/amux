## Why

`/_am/status` took ~700 ms even idle because every pool entry decrypted identities.json (~50 ms each), over the CLI's 500 ms liveness probe. `amux run` then believed the gateway was down, spawned a second one, and the failed start overwrote gateway.pid with the wrong pid. The relay-doubt detector also matched ordinary answers that mention "prompt injection" and "can't verify".

## What Changes

- Decrypted identities are cached by path, size and mtime (fresh unmarshal per call; cache dropped on save).
- The liveness probe retries patiently when the port is held, treating a busy gateway as running; `Start` no longer writes the supervisor's pid into gateway.pid.
- Relay doubt requires a relay-specific phrase plus a second signal.

## Capabilities

### Modified Capabilities
- `gateway-http`: liveness probe.
- `web-tool-optimization`: relay-doubt precision.

## Impact

- `pkg/identity/store.go`, `pkg/gateway/gateway.go`, `pkg/tools/harness.go`.
