## Why

`amux` currently carries redundant components (specifically the `muse.ai` browser automation driver and hardcoded agent heuristics) that bloat the codebase, introduce fragile DOM dependencies, and cause false-positive tool executions on web accounts. Cleaning up these components and refining the core tool execution engine will make `amux` significantly more reliable, lighter, and strictly focused on its mission as a Universal AI Gateway.

## What Changes

- **REMOVAL**: Remove `pkg/muse` browser automation module and all associated `muse_*` MCP tools and CLI commands.
- **REMOVAL**: Remove hardcoded task/skill heuristics (`open-pr:review`, `open-pr:fix`, `webapp-evidence:recording`, `webapp-evidence:vision`) from web provider prompts and webloop parsers.
- **IMPROVEMENT**: Refactor Web-Loop Tool Engine (`pkg/tools/webloop.go`) to use strict grammar block matching and robust JSON schema conversion, eliminating false-positive bash/diff executions when models respond with conversational prose.
- **IMPROVEMENT**: Streamline MCP server (`pkg/mcp`) to focus purely on gateway routing and pool queries (`amux_providers`, `amux_ask`, `amux_status`).

## Capabilities

### Modified Capabilities
- `mcp-server`: Streamlines tool definitions to exclude third-party browser automation tools while preserving pure AI routing tools (`amux_ask`, `amux_providers`, `amux_status`).
- `web-providers`: Removes hardcoded domain-specific prompt hacks and sanitizes web backend prompt formatting.
- `tool-dialects`: Enhances webloop tool parsing to strictly honor explicit tool markups and prevent false-positive command generation.
- `muse-web-provider`: Removes native muse automation provider from core runtime.

## Impact

- Removed packages: `pkg/muse/`.
- Updated packages: `pkg/mcp/`, `pkg/provider/`, `pkg/tools/`, `pkg/cli/`.
- Affected CLI commands: `amux login muse` and `amux mcp` (muse tools omitted).
- Zero breaking changes to standard AI Gateway endpoints (`/v1/messages`, `/v1/chat/completions`, `/v1/responses`, `/v1beta/models`).
