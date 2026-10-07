## Why

Web adapters kept one ChatGPT / Claude.ai / Gemini thread per project and reused it for any request that arrived without a matching session id. Two problems followed:

- A live thread re-sent the client's whole system prompt (~68k runes for Claude Code) on every turn, and when shrinking dropped the tool turns it skipped the delta path. ChatGPT read the follow-up as pasted text and answered "I have no repo access" instead of acting on "improve it".
- Once the system prompt is sent only on a thread's first turn, the thread must belong to that prompt. Claude Code sends side requests (title generation) with the same session id but a different system prompt; sharing the project thread would leave the main session running without its own instructions, and two Claude windows on one project kept replacing each other's thread.

## What Changes

- A web thread belongs to one client session and one system prompt: its key is the project plus a hash of the session id and the system turns (volatile billing header lines ignored). A new Claude Code session, a side request with another system prompt, or a changed system prompt opens a new thread whose first turn carries the system prompt and full transcript.
- A live thread gets only what it has not seen: the new user request, or the tool results since the last reply. Shrinking and the system prompt are kept for fresh threads and `FullContext` handoffs; the tool-session check runs on the unshrunk history.
- Snapshots are stored per thread (`conv_<adapter>_<hash>.json` in the project cache dir); resetting a project clears all of its session threads.

## Impact

- `pkg/provider/prompt.go` (`WebBackendPrompt`, `PromptWithSystem` removed).
- `pkg/provider/project_conv.go` (`ThreadKey`, per-thread snapshots, project-wide reset).
- `pkg/provider/chatgpt_web.go`, `claude_web.go`, `gemini_web.go` key threads with `ThreadKey`.
