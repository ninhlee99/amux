## Why

`/open-pr:fix` through ChatGPT Web ended after 17 seconds with "It seems like I can’t do more advanced data analysis right now. Please try again later." The raw stream shows ChatGPT calling its own `python` tool (`content_type: code`, recipient `python`), the tool answering `system_error`, and ChatGPT's canned reply. amux already redirected the `container.*` sandbox to the client's shell tool, but not `python`, and the canned reply was not seen as a refusal.

## What Changes

- With client tools, a finished ChatGPT message to `python` with `content_type: code` becomes a Bash tool call running the code via `python3 - <<'AMUX_PY' … AMUX_PY`; the thread holding the sandbox error is dropped.
- "can't do more advanced data analysis" counts as a tool refusal (nudge retry).

## Capabilities

### Modified Capabilities
- `web-providers`: ChatGPT python tool redirect.

## Impact

- `pkg/provider/chatgpt_web.go`, `pkg/tools/webloop.go`.
