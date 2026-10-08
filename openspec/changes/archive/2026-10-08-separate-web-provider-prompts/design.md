## Context

AMUX adapts multiple web providers (`ClaudeWebAdapter`, `ChatGPTWebAdapter`, `GeminiWebAdapter`) to serve IDE clients. Each provider communicates with a different web interface with its own behavioral characteristics and model alignment. Sharing a single static prompt template across all three creates friction (e.g. Claude is confused by Python sandbox warnings, whereas ChatGPT needs them).

## Goals / Non-Goals

**Goals:**
- Provide specialized prompt preambles, reminders, and closers tailored for:
  - Claude Web (`claude`): Clean, neutral agent tool protocol without container sandbox mentions.
  - ChatGPT Web (`chatgpt`): Includes steering away from built-in Python / Advanced Data Analysis container.
  - Gemini Web (`gemini`): Google-friendly workspace tool instructions.
- Expose `WebBackendPromptForProvider(provider, req, continuing)` in `pkg/provider`.
- Update `claude_web.go`, `chatgpt_web.go`, and `gemini_web.go` to use provider-specific prompts.
- Maintain backwards compatibility for existing callers of `WebBackendPrompt`, `WebPreamble`, etc.

**Non-Goals:**
- Altering the parsing or stream wrapping logic (`wrapWebStream`, AST auto-repair).
- Removing multi-schema JSON parsing support.

## Decisions

### Decision 1: Provider String Normalization in `pkg/tools`
Define helper `NormalizeWebProvider(provider string) string` returning `"claude"`, `"chatgpt"`, or `"gemini"`.
When empty or unknown, default to `"chatgpt"` (the safe default with general sandbox guidance).

### Decision 2: Distinct Templates per Provider
- `webToolPreambleClaude`:
  ```
  You are an AI programming assistant with tool execution capability.
  To inspect files, execute terminal commands, or edit code in the workspace, output a <tool_call> block:
  <thought>
  Optional reasoning / thinking step
  </thought>
  <tool_call>
  {"name":"TOOL_NAME","arguments":{...}}
  </tool_call>
  After emitting tool calls, wait for [Tool result] before continuing.
  Example:
  User: check git status
  Assistant: <tool_call>
  {"name":"Bash","arguments":{"command":"git status"}}
  </tool_call>

  Multiple blocks OK. CATALOG
  ```
- `webToolPreambleChatGPT`: Keeps the existing warning that built-in python/container cannot see the workspace.
- `webToolPreambleGemini`: Formatted specifically for Gemini StreamGenerate.

Similarly for `webToolReminder` and `webToolCloser`.

## Risks / Trade-offs

- [Risk] Calling code doesn't pass provider identifier.
  → *Mitigation*: Fall back gracefully to default templates so existing code never breaks.
