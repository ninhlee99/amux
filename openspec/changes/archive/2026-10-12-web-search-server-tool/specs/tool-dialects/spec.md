## ADDED Requirements

### Requirement: Server-side web search is emulated for web accounts
For an Anthropic Messages request that declares a `web_search_*` server tool, the gateway SHALL have the serving account search and answer, and SHALL respond with a `server_tool_use` block (the query), a `web_search_tool_result` block listing each cited source's title and URL, and the answer text, in both streaming and non-streaming form.

#### Scenario: Claude Code WebSearch through ChatGPT Web
- **WHEN** Claude Code runs WebSearch for "IANA example domain" through amux
- **THEN** the tool result lists links such as "Example Domains - IANA" with their URLs
