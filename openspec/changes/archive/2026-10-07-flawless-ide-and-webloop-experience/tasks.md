# Implementation Tasks

- [x] 1. Daemon Logging & Observability <!-- id: 1-daemon-logging -->
  - [x] 1.1 In `pkg/gateway/gateway.go`, update `Start()` to redirect detached daemon Stdout/Stderr to `~/.amux/gateway.log`
  - [x] 1.2 In `pkg/telemetry/logger.go`, ensure all logging paths target `types.BaseDir()` (`~/.amux/gateway.log`)
  - [x] 1.3 In `pkg/gateway/gateway.go` `RunDaemon` and `pkg/proxy/server.go` `RunProxy`, initialize logging on daemon startup

- [x] 2. Zero-Pollution Sandbox & Provider Pinning <!-- id: 2-sandbox-and-pinning -->
  - [x] 2.1 In `pkg/proxy/server.go`, extract provider overrides from URL path (`/p/{provider}/...`, `/provider/{provider}/...`) and query parameters (`?provider={provider}`)
  - [x] 2.2 In `pkg/cli/run.go`, parse `--provider` / `-p` flags and construct sandbox URLs with `/p/{provider}` prefix
  - [x] 2.3 In `pkg/cli/run.go`, eliminate automatic on-disk settings mutations unless `--setup` is passed

- [x] 3. Web Tool Semantic Handling, Thinking Streaming & Output Pruning <!-- id: 3-web-tools-and-prune -->
  - [x] 3.1 In `pkg/tools/webloop.go`, preserve lean catalog token design and support Action/Input formats
  - [x] 3.2 In `pkg/tools/prune.go`, increase pruning thresholds to 24,000 bytes and 300 lines
  - [x] 3.3 In `pkg/tools/webloop.go`, stream live `<thought>` / `<thinking>` content as it arrives in `wrapWebStream`

- [x] 4. Verification & Testing <!-- id: 4-verification -->
  - [x] 4.1 Run unit and integration tests across modified packages (`pkg/gateway`, `pkg/proxy`, `pkg/cli`, `pkg/tools`)
  - [x] 4.2 Validate OpenSpec strict compliance with `openspec validate --all --strict`

