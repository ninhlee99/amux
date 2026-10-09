## Context

Web adapters have no native tool API: amux flattens the client request into one prompt and parses `<tool_call>` blocks back into tool calls. Claude Code ends its turn on any text-only reply, so a refusal or a stall leaves the user's remaining steps unrun.

## Decisions

### Text-protocol framing for ChatGPT
ChatGPT checks claims about its environment against its own function list. Telling it that it *has* tools fails that check; telling it that a `<tool_call>` block is plain text the client runs passes it. Claude and Gemini prompts are unchanged (both already pass).

### Optional args are marked, invalid ones dropped
The catalog line becomes `Agent:description:string,prompt:string,isolation?:worktree|remote,...`. Prompting alone is not enough, so `coerceToolArgs` also drops optional args whose value is null or outside the schema enum; required args are never touched.

### One nudge retry, held content
`sendWithWebNudge` wraps each adapter's single send. For requests with tools it holds content chunks (thinking streams through) until the turn is decided. With no real tool call and `IsToolRefusal` or `IsToolStall`, it re-sends once with a user turn: protocol reminder + current task. A stall keeps its text as an assistant turn (the live thread holds it); a refusal's thread is reset by the adapter and the refusal is not replayed. At most one retry per client request.

`IsToolStall` matches short replies (≤600 runes) announcing an action (`I need to continue`, `Let me run`, `Now I'll fix`, `remaining steps`) and excludes offers (`If you want, I'll…`, `would you like`).

### Thread checkpoints ignore gateway turns
`ChatRequest.ClientMessages` records how many messages came from the client. Adapters register turns with `ClientHistoryMark(req)`, so the client's next request matches the latest checkpoint instead of forking back to the stalled turn.

## Risks

- Holding content delays visible prose for tool-enabled requests until the web reply finishes; thinking still streams.
- A false-positive stall costs one extra web call; the nudge says to answer if every step is done.
