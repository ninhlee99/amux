## Why

Running `/open-pr:fix` through Claude Web, claude.ai accepted completion requests and then sent no bytes for 17–25 minutes at a time; Claude Code sat on "Mustering…" until it cancelled the request itself. The shared web HTTP client bounds only the wait for response headers (5 min); once a stream is open, silence is unbounded.

## What Changes

- Web adapter HTTP clients (the shared default client and per-account egress proxy clients) wrap response bodies with an idle watchdog: a body that delivers no bytes for 3 minutes is closed and reads fail with "upstream stream idle". Total stream length stays unbounded.

## Capabilities

### Modified Capabilities
- `web-providers`: stream idle timeout.

## Impact

- `pkg/provider/http_client.go`, `pkg/provider/config.go`.
