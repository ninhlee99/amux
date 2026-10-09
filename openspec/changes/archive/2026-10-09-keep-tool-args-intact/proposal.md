## Why

Driving `/webapp-evidence:recording` and `/open-pr:review` through `amux run claude -p <web>` corrupted tool arguments twice:

- Outbound privacy redaction took Playwright's raw capture `page@<hash>.webm` for an email and sent `sample@example.com`, so the model tried to rename a file that does not exist. `noreply@anthropic.com` commit trailers were rewritten too.
- Gemini Web wraps domain-like text in self-links, including inside `<tool_call>` JSON: a Write to `[github.com/o/r/x.json](https://github.com/o/r/x.json)` created that literal directory, and `--repo` received the link.

## What Changes

- Email redaction keeps matches whose "TLD" is a file extension, and no-reply addresses.
- Gemini Web text has self-links (label equal to the URL, with or without scheme) collapsed before it is streamed or parsed.

## Capabilities

### Modified Capabilities
- `gateway-http`: email redaction exemptions.
- `web-providers`: Gemini self-link collapsing.

## Impact

- `pkg/privacy/redact.go`, `pkg/provider/gemini_web.go`.
