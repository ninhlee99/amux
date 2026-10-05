## Why

Following our deep architectural audit of `amux`, several key areas require refinement to make tool execution 100% deterministic, eliminate false-positive tool forcing from conversational prose, streamline onboarding/CLI ergonomics, and guarantee zero-touch hook cleanup upon shutdown so developers never experience broken IDE networking.

## What Changes

- **Refine Web-Loop Tool Engine**: Remove invasive heuristic guessing of file paths and shell commands when models produce refusal/conversational prose. Preserve 100% accurate extraction of explicit tool blocks (`<tool_call>`, `[tool_call]`, `<<<AMUX_TOOL>>>`, ````json / ````tool_call) with robust JSON repair.
- **Enhanced IDE Auto-Cleanup**: Ensure `amux stop`, `amux off`, and `amux hook --unhook` perform bulletproof idempotency and remove all proxy hooks so IDEs never get stranded with dead `127.0.0.1:8787` settings.
- **Streamlined MCP & CLI Ergonomics**: Ensure `amux mcp install --all` and target-specific commands provide crystal-clear diagnostic feedback and clean error paths.

## Capabilities

### New Capabilities
<!-- No new capabilities needed -->

### Modified Capabilities
- `tool-dialects`: Refine web-loop extraction to only parse explicit tool call markup and avoid fictitious tool guessing.
- `ide-integration`: Enhance unhooking and shutdown guarantees across all IDE targets.

## Impact

- Affected packages: `pkg/tools/webloop.go`, `pkg/gateway/hook.go`, `pkg/cli/`, `pkg/mcp/`.
- No breaking API changes; strictly improves stability, predictability, and developer experience.
