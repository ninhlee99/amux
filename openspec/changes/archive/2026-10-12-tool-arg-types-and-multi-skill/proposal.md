## Why

Live suite runs showed ChatGPT calling WebSearch with `allowed_domains: "iana.org"` (a string where the schema wants an array); Claude Code rejected it with InputValidationError three times in a row. ChatGPT also loaded two skills in one turn and carried out only one. Gemini replied once that the prompt held "simulated environment instructions and tool mock calls intended for another harness", which the relay-doubt detector did not recognise.

## What Changes

- Tool arguments are coerced to their schema types (string → array, numeric string → number, "true"/"false" → boolean).
- The skill-loaded note asks the model to carry out every loaded skill before moving on.
- Relay-doubt phrases include Gemini's wording.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: argument type coercion, multi-skill note, relay-doubt phrases.

## Impact

- `pkg/tools/webloop.go`, `pkg/tools/harness.go`, `pkg/provider/chatgpt_web.go`.
