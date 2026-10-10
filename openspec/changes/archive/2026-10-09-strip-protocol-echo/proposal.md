## Why

During `/open-pr:review` through Gemini Web, Claude Code showed "Wait for [Tool result] before continuing." as the assistant's text before most tool calls: Gemini copies protocol lines of the tool preamble into its prose.

## What Changes

- `StripWebToolMarkup` and `StripInternalThoughtAndToolTags` drop echoed protocol lines ("Wait for [Tool result] before continuing.", "Multiple blocks OK.", "Several blocks only for independent calls…", "Then stop and wait for [Tool result].").

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: protocol echo stripping.

## Impact

- `pkg/tools/webloop.go`.
