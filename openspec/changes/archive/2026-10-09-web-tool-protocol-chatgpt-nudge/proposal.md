## Why

A live test of Claude Code through `amux run claude -p <web account>` (probe: Write, Bash, Edit, Agent subagent, project skill, plugin skill, MCP tool) showed Gemini Web passing all steps, and ChatGPT Web passing none:

- ChatGPT (gpt-6-luna) read "you have access to workspace tools" as a claim about its own native functions, did not find them, and refused on the first turn.
- The refusal fallback kickstarted `mcp__amux__amux_fix` with empty required args, because the goal mentioned both "mcp" and "fix" and any "mcp" in the goal disabled the Bash fallbacks.
- Once tools ran, ChatGPT filled every catalog key of `Agent` (`isolation:"none"/"remote"`, `model:"claude-haiku-…"`), so every subagent call was rejected.
- A loaded skill body became the `[next] … Task:` line, so the user's remaining steps were lost, and ChatGPT ended with "I need to continue by running the remaining required tool steps." with no tool call, which ends Claude Code's turn.
- The first request of a session (system + user) got the `[xfer] Continue previous task` handoff.
- After a skill loaded, ChatGPT treated "Launching skill" as done and reported output of the skill's commands without running them.
- Gemini Web wraps bare URLs in self-links (`[http://x](http://x)`), which reach the client as literal brackets.

## What Changes

- ChatGPT tool preamble/reminder/closer frame `<tool_call>` as a text protocol the client parses: no native tools are needed, so "tools unavailable" is never true.
- The web catalog marks optional args `?` and lists enum values; coercion drops optional args outside their enum or null.
- New `sendWithWebNudge` (all three web adapters): a reply with no real tool call that is a refusal or a stall is re-sent once with a nudge turn restating the protocol and the task.
- `ChatRequest.ClientMessages` + `ClientHistoryMark` keep thread checkpoints on the client's history, so the nudge turn is not seen as a fork.
- Skill bodies in the flattened transcript are headed with a note that loading a skill does not perform it.
- Gemini self-links (label == URL) are collapsed back to the bare URL.
- `currentUserTask` skips skill-expansion turns; the `[xfer]` handoff needs more than one non-system turn.
- Refusal kickstart: a tool the user named verbatim wins, tools whose required args would be empty lose; "mcp" in the goal no longer disables Bash fallbacks when the goal also names a non-MCP tool.

## Capabilities

### Modified Capabilities
- `web-tool-optimization`: catalog optional/enum marking, text-protocol framing for ChatGPT, task cue ignores skill bodies.
- `webloop-self-healing`: one nudge retry on refusal/stall; refusal kickstart picks a runnable tool.

## Impact

- `pkg/tools/webloop.go`, `pkg/provider/prompt.go`, `pkg/provider/web_nudge.go`, `pkg/provider/project_conv.go`, the three `*_web.go` adapters, `pkg/types/chat.go`.
