## ADDED Requirements

### Requirement: Request trace id
The gateway SHALL give every request an id in `X-Amux-Request-Id`, keeping a caller-supplied id only when it is at most 64 characters of `[A-Za-z0-9_-]`. The id MUST be returned on the response, written on the request's log line, request entry and `errors.log` block, appended to client-facing error messages, and MUST NOT be forwarded to any upstream.

#### Scenario: Failed turn names its id
- **WHEN** a `/v1/chat/completions` request fails because every pool account errored
- **THEN** the error message ends with `[amux request <id>]` and `errors.log` has a block with `req=<id>`

#### Scenario: Id stays local
- **WHEN** Claude Code is reverse-proxied to Anthropic
- **THEN** the client receives `X-Amux-Request-Id` and the Anthropic request carries no such header

### Requirement: Live end-to-end check
`amux doctor --live` SHALL send real turns through the running gateway — an Anthropic Messages turn, a streamed OpenAI Chat Completions turn and a Bash tool roundtrip — optionally pinned with `--provider <id|family>`, and print for each check its result, request id and suggested fix commands. It MUST stop after the gateway probe when the gateway is not answering.

#### Scenario: Web account answers in prose
- **WHEN** the serving account replies to the tool roundtrip with text and no `tool_use`
- **THEN** that check fails and suggests retrying per account with `--provider`

#### Scenario: Gateway down
- **WHEN** nothing answers at the gateway address
- **THEN** only the gateway check is shown, failed, with `amux start` as the fix
