## Why

Live Claude Web runs answered Claude Code's WebSearch with "I don't have web search access in this environment": claude.ai does not search unless its own web-search feature is on, so the emulated web_search returned no sources.

## What Changes

- When the serving account's search answer cites no sources, the web_search side request (stateless) is re-sent to an account family that searches natively (ChatGPT, then Gemini Web) and that answer is used.

## Capabilities

### Modified Capabilities
- `tool-dialects`: web search fallback.

## Impact

- `pkg/bridge/websearch.go`.
