## ADDED Requirements

### Requirement: Web search falls back to an account that can search
When the account serving a web_search side request answers without citing any source, the gateway SHALL re-send that request to another account family that searches natively (ChatGPT, then Gemini Web) and use the first answer that cites sources.

#### Scenario: pinned Claude Web session runs WebSearch
- **WHEN** Claude Web replies that it has no web search access
- **THEN** the WebSearch result lists sources found through ChatGPT Web
