## Why

Developers using AMUX face friction and fragility in two critical areas:
1. When IDEs are hooked to the local gateway, an offline or crashing daemon immediately causes `ECONNREFUSED` network failures, breaking developer flow.
2. Web account tool loops (ChatGPT Web, Gemini Web, Meta Muse) can suffer from malformed JSON or refusal text during multi-turn agent sessions, while MCP tools lack contextual project integration and advanced analysis tools like multi-file git diff review.

Upgrading AMUX with transparent gateway fail-safe routing, self-healing web tool error recovery, robust context retention, and full-context MCP 2.0 tools creates a seamless, bulletproof, and delightful developer experience.

## What Changes

- **Gateway Transparent Pass-Through & Fail-Safe**: Provide automatic fall-safe forwarding to native upstream APIs via stored keychain credentials if the local gateway router is unable to serve the request, preventing IDE network errors.
- **Self-Healing Web Tool Engine**: Enhance `webloop.go` with robust fuzzy JSON argument repair and automatic schema-guided correction for web model responses.
- **Enhanced MCP Suite (MCP 2.0)**: Add `amux_review` (or `muse_review`) multi-file diff reviewer tool, and enhance `amux_ask` to support workspace context parameters.
- **Meta Muse & Web Session Resilience**: Improve CDP keep-alive and error propagation for web providers.

## Capabilities

### New Capabilities
- `resilience-failsafe`: Transparent fallback mechanism to prevent IDE request drops when the gateway daemon encounters errors or pool exhaustion.
- `mcp-extended-suite`: Extended MCP tool suite providing multi-file code review and enhanced contextual prompts.

### Modified Capabilities
- `web-tool-optimization`: Enhanced fuzzy JSON repair, alias coercion, and self-healing extraction for web models.
- `mcp-server`: Extended tool definitions and context awareness for MCP clients.

## Impact

- `pkg/proxy/server.go`, `pkg/proxy/passthrough.go`: Fail-safe interceptor logic.
- `pkg/tools/webloop.go`: Fuzzy JSON repair & enhanced dialect coercion.
- `pkg/mcp/tools.go`, `pkg/mcp/backend.go`: New MCP tools (`amux_review`, enhanced `amux_ask`).
- `pkg/provider/`: Web provider resilience and stream error handling.
