## Why

When a model loads a command through the Skill tool (plugin or user commands such as `dev-workflow:status`), Claude Code injects the command body with no "Base directory for this skill:" header. The web prompt then showed it as an ordinary user turn without the skill note, and the mid-task cue named it as the task ("Task: Show current status."), replacing the user's request. ChatGPT lost the multi-step task and never ran the skill.

## What Changes

- A user turn right after a `Launching skill:` tool result counts as a skill body: it gets the skill-loaded note and is skipped when picking the current task.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: command expansions are skill bodies.

## Impact

- `pkg/provider/prompt.go`, `pkg/provider/chatgpt_web.go`.
