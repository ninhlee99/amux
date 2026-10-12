## Why

Claude Code's WebSearch tool sends a side request whose only tool is Anthropic's server-side `web_search_20250305`. Through amux the web model answered with plain text, so Claude Code received "Web search results for query: …" without any source links, and models reported "no result titles".

## What Changes

- The Claude bridge detects a `web_search_*` server tool, drops it from the catalog, asks the web model (which searches natively) to answer and list sources as markdown links, and returns `server_tool_use` + `web_search_tool_result` (title/url per source) + text, streamed or JSON.

## Capabilities

### Modified Capabilities
- `tool-dialects`: Anthropic server-side web search emulation.

## Impact

- `pkg/bridge/websearch.go`, `pkg/bridge/claude.go`.
