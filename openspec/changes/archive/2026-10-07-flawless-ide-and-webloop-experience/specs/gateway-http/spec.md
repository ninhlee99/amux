## ADDED Requirements

### Requirement: Path-based and query-based provider routing
The gateway SHALL support provider selection via URL path prefixes (`/p/{provider}/...` or `/provider/{provider}/...`) and query parameters (`?provider={provider}`) in addition to the `X-Provider` HTTP header. The path prefix SHALL be stripped before routing to standard endpoint handlers (`/v1/messages`, `/v1/chat/completions`, etc.).

#### Scenario: Path-based provider routing
- **WHEN** a client sends a request to `POST http://127.0.0.1:8787/p/gemini:web:01/v1/messages`
- **THEN** the gateway routes the request specifically to `gemini:web:01` without requiring custom HTTP headers

#### Scenario: Query-based provider routing
- **WHEN** a client sends a request with `POST http://127.0.0.1:8787/v1/chat/completions?provider=chatgpt`
- **THEN** the gateway routes the request specifically to the `chatgpt` family

### Requirement: Detached daemon logging
The background gateway daemon SHALL redirect standard output and standard error to `~/.amux/gateway.log` with file mode `0644`.

#### Scenario: Daemon logging verification
- **WHEN** `amux start` launches the gateway daemon
- **THEN** startup events and background errors are recorded in `~/.amux/gateway.log`
