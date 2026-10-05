## Context

amux is a Go CLI + local gateway (`:8787`). Integration with agents was env/config hooks only. Web backends talk HTTP to chat sites with captured cookies. Meta Muse has no HTTP API: its chat runs over an end-to-end encrypted Noise WebSocket whose keys live in the web app. The reference project `muse-chat-mcp` (Node + Playwright) proved that driving the real page works; per the user's request amux re-implements the idea from scratch in Go instead of embedding Node.

## Goals / Non-Goals

**Goals:**
- Any MCP-capable agent can use every chat platform amux manages, with zero per-IDE code beyond writing a config entry.
- Muse usable both as a gateway backend (Claude Code/Codex/Cursor traffic) and as rich MCP tools (chats, attachments, generated media).
- A mode in which amux never touches the OS keychain.
- Every default/advertised model id exists upstream today.

**Non-Goals:**
- Re-implementing Muse's Noise transport.
- Running tools inside the gateway (tools stay client-side).
- Changing native keychain rotation for users who keep keychain mode.

## Decisions

1. **MCP over stdio, hand-written JSON-RPC.** No SDK dependency; requests are handled concurrently (a long `muse_chat` never blocks `ping`), `notifications/cancelled` cancels the call's context, `_meta.progressToken` yields throttled `notifications/progress`. Tool failures are returned as `isError` results so the model can react. Serve mode redirects `os.Stdout` to stderr and keeps the real stdout for the protocol, so stray prints can never corrupt the stream.
2. **`amux_ask` runs the pool in-process** with the same `AccountPoolRouter`, but automatic selection excludes subscription accounts (they are usually the calling agent's own plan, and amux never enables subscriptions implicitly); an explicit `provider` pin or `AMUX_MCP_ALLOW_SUBSCRIPTION=1` reaches them.
3. **One browser per profile, one tab per process.** The CDP session launches Chromium with `--remote-debugging-port=0`; other amux processes find it through `<profile>/DevToolsActivePort` and attach, opening their own tab. A process only kills a browser it launched. Background-throttling flags plus focus emulation keep replies rendering in unfocused tabs.
4. **Muse driver = DOM polling with injected scripts.** Selectors use Muse's `data-*` attributes. Prompts go in via `Input.insertText` (fallback `execCommand`), Enter via `Input.dispatchKeyEvent`, files via `DOM.setFileInputFiles` (http/data URLs materialised to temp files). Streaming only emits text that is stable for `QuietPeriod` and a prefix-extension of what was already sent, so rewritten drafts never duplicate. The trailing `Assistant:` cue of the shared web prompt is stripped because Muse rejects role-play transcripts.
5. **Muse threading follows the web-provider convention:** `FullContext` → fresh side chat; otherwise per-project thread reuse via `ProjectConversationManager`. The adapter waits for the first text (or the outcome) before returning so failures before output are synchronous and fail over.
6. **Secret store is a seam under `KCGet/KCSet/KCAccount`.** All existing callers keep working; file mode stores `[service, account, secret]` items in `~/.amux/secrets.vault` (`AMENC1:` AES-256-GCM, key from `$AMUX_MASTER_KEY` or `~/.amux/master.key`). Claude credentials are mirrored to `~/.claude/.credentials.json`, the file Claude Code itself uses without a keychain. Switching to file first exports the keyring master key so sealed files stay readable, then copies the keychain items amux uses — the last keychain read. Test binaries default to file mode.
7. **Model ids verified at the source** (2026-10-05): Anthropic `claude-opus-5-5` default / `claude-sonnet-5-5` gateway default; OpenAI & Codex `gpt-6-astra`, `gpt-6.1-sol` (default), `gpt-6-luna`; Gemini `gemini-3.8-flash`, `gemini-3.1-pro-preview`; Groq `llama-3.3-70b-versatile`, `openai/gpt-oss-120b`; xAI `grok-4.7`; Kimi `kimi-k2.7-code` at `https://api.moonshot.ai/v1`. Claude payloads drop `temperature` from 4.7+, use adaptive thinking from 4.6+, and downgrade forced `tool_choice` to `auto` where it is rejected. A source scan test blocks retired ids from returning.

## Risks / Trade-offs

- Muse UI changes can break selectors → `muse_dump_dom` exposes counts/HTML for quick re-harvesting; selectors live in one table.
- Browser automation is slower than HTTP → Muse sits at priority 27 (after HTTP web sessions).
- ChatGPT web keeps model `auto` (the web app's own routing) because web slugs cannot be verified without a session; `gpt-6-luna` is shown as its model.
- File secret store on macOS stops native (gateway-less) rotation for Claude Code → documented; users route Claude Code through the gateway (`amux hook --claude`).
- Rewriting JSON host configs loses key order → backups (`.amux.bak`) are kept and JSONC files (comments) are never rewritten; the snippet is printed instead.
