# amux

Keep several AI accounts — Claude Code, Codex, Antigravity subscriptions, ChatGPT / Gemini / Meta Muse web sessions, API keys — on one machine and switch between them without logging in again. Optionally route tools through a local gateway, or expose the accounts as MCP tools to any agent.

amux changes nothing on its own: a tool's login, config or account only changes when you run a command for it, and every change can be undone exactly.

## Install

Requires Go 1.22+ on macOS or Linux.

```bash
git clone https://github.com/ninhlee99/amux.git && cd amux
go build -o amux . && sudo cp amux /usr/local/bin/
amux help
```

## Switch accounts without logging in again

```bash
amux login claude          # log in account A (browser); it becomes Claude Code's login
amux login claude          # log in account B; A is saved first
amux account list          # see both
amux switch claude:code:01 # Claude Code is back on account A — no browser
```

```
ID                       EMAIL              TYPE  ACTIVE  POOL  USAGE   THRESHOLD  RESETS IN
claude:code:01           alice@company.com  sub   yes     no    12%     100.0%     4h10m
claude:code:02           alice@gmail.com    sub   -       no    0%      100.0%     unknown
```

- Works the same for `codex` and `agy` (Antigravity). `amux switch` with no id shows a numbered picker.
- The login you leave is saved first, so refreshed tokens are never lost and switching back always works.
- Only the account keys are swapped: for Claude Code that is the keychain item and `oauthAccount` in `~/.claude.json`; MCP servers, projects and settings in that file stay as they are.
- Running sessions keep their account until restarted (`claude --continue` resumes).
- If a switch fails, that account's login has expired: `amux login <tool>` once more.

## Rotation pool

The pool is the set of accounts amux may switch between **by itself** when one reaches its limit.

```bash
amux pool                                    # who is in / out
amux pool add claude:code:01 claude:code:02  # allow automatic switching between them
amux pool remove claude:code:02              # back to manual only
```

Rules:

1. Subscriptions (Claude Code, Codex, Antigravity) are **never** in the pool unless you add them. Logging in does not add them.
2. amux only switches from a pooled account to another pooled account. An account outside the pool is never switched to, and never switched away from, automatically — when it hits its limit, it stays limited until you `amux switch`.
3. Web and API-key accounts are in the pool by default; take one out with `amux pool remove <id>`.
4. A pooled account hands over at 95% usage (100% when it is the only pooled account of its provider). Change it with `amux account threshold <id> <pct>` or globally with `amux threshold <pct>`.
5. Automatic switching happens for traffic that goes through the gateway (below). Without the gateway, nothing is switched automatically.

`amux account off <id>` turns an account off entirely (never used, not even by `amux switch`) until `amux account on <id>`.

## Gateway (optional)

A local server on `http://127.0.0.1:8787` that speaks the Anthropic, OpenAI (chat + responses) and Gemini APIs and serves them from your accounts.

```bash
amux start            # run it in the background
amux hook claude      # point Claude Code at it (also: codex, cursor, agy, all)
amux status           # gateway, hooked tools, accounts
amux unhook claude    # undo one tool
amux off              # unhook every tool and stop the gateway
```

What `hook` writes — and `unhook` removes again, leaving everything you set yourself:

| Tool | File | Keys |
| :--- | :--- | :--- |
| Claude Code | `~/.claude/settings.json` | `env.ANTHROPIC_BASE_URL`, `env.ANTHROPIC_AUTH_TOKEN` (placeholder) |
| Codex | `~/.codex/config.toml` | `openai_base_url` |
| Cursor | Cursor `User/settings.json` | `cursor.openaiBaseUrl` |
| Antigravity | `~/.gemini/antigravity-cli/settings.json`, a marked block in your shell rc, launchctl | `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_KEY` (only if unset), `modelProvider` (only if unset) |

A hooked tool needs the gateway running. `amux stop` warns about tools that are still hooked; `amux off` is the one-step way back to native.

Pin a request to one account or one family with the `X-Provider` header (`gemini:web:01` = that account only; `gemini:web`, `chatgpt`, `muse:web` = amux picks a healthy one in that family):

```bash
curl http://127.0.0.1:8787/v1/chat/completions -H "Content-Type: application/json" \
  -H "X-Provider: gemini:web" -d '{"model":"auto","messages":[{"role":"user","content":"hi"}]}'
```

## MCP

Use your accounts as tools from any MCP-capable agent: Claude Code, Claude Desktop, Cursor, Windsurf, VS Code (Copilot agent), Gemini CLI, Antigravity, Codex, opencode, Zed.

```bash
amux mcp install all       # register in every detected host (or name hosts: claude cursor …)
amux mcp status            # where it is registered
amux mcp uninstall         # remove it from every host it is registered in
```

Install edits only the `amux` entry, keeps a `.amux.bak` of the original until uninstall, and never rewrites a config with comments (it prints the snippet instead: `amux mcp config <host>`). Uninstall removes the entry, the backup, and the file itself if amux created it.

| Tool | Purpose |
| :--- | :--- |
| `amux_ask` | Ask another model. `provider` = an exact id (pinned) or a family (`gemini:web`, `chatgpt`, `muse:web`). Subscriptions are used only when pinned or in the pool. |
| `amux_providers`, `amux_status` | Usable accounts; gateway URLs per client |
| `muse_chat`, `muse_new_chat`, `muse_chats`, `muse_open_chat`, `muse_read_chat`, `muse_read_last`, `muse_media` | Meta Muse chat, history, attachments, generated media |
| `muse_status`, `muse_login`, `muse_dump_dom`, `muse_close` | Muse session and diagnostics |

## Other accounts

```bash
amux login chatgpt      # ChatGPT web session
amux login gemini-web   # Gemini web session
amux login muse         # Meta Muse, in amux's own browser profile (no keychain)
amux login groq --token gsk_...   # API keys: gemini, groq, kimi, grok, github, cursor
```

Meta Muse has no API; amux drives the web app over the Chrome DevTools Protocol. `AMUX_MUSE_CDP=http://127.0.0.1:9222` attaches to a Chrome you already run; `AMUX_MUSE_HEADLESS=1` runs headless after the first login.

## Secrets

Everything in `~/.amux` is encrypted (AES-256-GCM, files `0600`); the gateway listens on `127.0.0.1` only. The master key lives in the OS keychain, or — after `amux config secret-store file` — in `~/.amux/master.key` (or `$AMUX_MASTER_KEY`), so amux never touches the keychain. Details: [docs/security-model.md](docs/security-model.md).

## Removing amux

| Command | Removes | Keeps |
| :--- | :--- | :--- |
| `amux unhook [tool]` | The gateway keys `hook` added | Everything else in the tool's config |
| `amux off` | All hooks; stops the gateway | Accounts, MCP registrations |
| `amux mcp uninstall [host]` | The `amux` MCP entry and its backups | Other MCP servers |
| `amux account remove <id>` | That account and its saved login | The tool's current login |
| `amux uninstall` | Hooks, gateway, session hooks / status lines, MCP entries, `/amux` slash commands, auto-update agent, the binary | `~/.amux` |
| `amux uninstall --purge` | All of the above, `~/.amux`, amux's master key in the keychain | — |

None of these touch the tools' own logins: Claude Code, Codex and Antigravity keep working with whatever account they are on. Restart open sessions afterwards.

## Commands

| Command | Does |
| :--- | :--- |
| `amux login [provider]` | Add an account |
| `amux account list` (`amux ls`) | List accounts |
| `amux switch [id]` | Make an account the active login of its tool |
| `amux account off\|on <id>` | Turn an account off / on |
| `amux account remove <id>` | Delete an account |
| `amux account threshold [id] [pct]` | Hand-over threshold for pooled accounts |
| `amux account health` | Check every saved login |
| `amux pool [add\|remove] <id>` | Manage the rotation pool |
| `amux start \| stop \| restart` | Run the gateway (`start -f` foreground) |
| `amux hook [tool]`, `amux unhook [tool]` | Route a tool through the gateway / undo |
| `amux off` | Unhook all and stop the gateway |
| `amux status` | Gateway, hooks, accounts |
| `amux mcp install\|uninstall\|status\|config\|tools` | MCP server and host registration |
| `amux usage [day\|week\|month]` | Token usage per account |
| `amux doctor` | Diagnose keychain, tools, gateway |
| `amux config secret-store file\|keychain` | Where secrets are kept |
| `amux update`, `amux uninstall [--purge]` | Update / remove amux |

`amux <command> --help` shows details and examples for each.

## Troubleshooting

- **A tool fails right after `amux stop`** — it is still hooked: `amux start`, or `amux off` to go native.
- **`amux switch` says the login expired** — `amux login <tool>` for that account once; switching works again afterwards.
- **Port 8787 in use** — `lsof -i :8787`, stop the other process, `amux start`.
- **Something else** — `amux doctor`, then `amux feedback` to file an issue.

## License

MIT — see [LICENSE](LICENSE).
