# AMUX (Universal AI Gateway)

<p align="center">
  <img src="https://raw.githubusercontent.com/ninhlee99/amux/main/docs/assets/banner.png" alt="AMUX Banner" width="100%" onerror="this.style.display='none'"/>
</p>

<p align="center">
  <a href="https://github.com/ninhlee99/amux/releases"><img src="https://img.shields.io/github/v/release/ninhlee99/amux?color=blue&label=version" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.22+-00ADD8?logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/platform-macOS%20(Apple%20Silicon%20%26%20Intel)-lightgrey?logo=apple" alt="Platform">
  <img src="https://img.shields.io/badge/tests-passing-brightgreen" alt="Tests">
</p>

<p align="center">
  <b>Universal AI Gateway & Multi-Account Rotation for AI Coding Agents.</b><br>
  Keep several AI accounts — Claude Code, Codex, Antigravity subscriptions, ChatGPT / Gemini / Claude web sessions, API keys — on one machine and switch between them without logging in again.
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> • <a href="README_VI.md"><b>Tiếng Việt</b></a>
</p>

---

## ⚡ Key Highlights

- **Zero-Pollution Sandbox (`amux run <ide>`)**: Run Claude Code, Cursor, Windsurf, Antigravity (AGY), or Codex in an isolated child process pointing to the gateway. Exiting leaves your shell and system settings 100% clean. Direct launches (`claude`, `cursor`) run natively with 0ms overhead.
- **Silent Keychain Rotation**: Maintain multiple accounts per provider. When usage hits 95%, AMUX smoothly rotates credentials or hands over to the next account without breaking your flow.
- **Web Accounts as Coding Proxies (WebLoop 2.0)**: Connect ChatGPT Web, Gemini Web, Claude Web, or Meta Muse. WebLoop translates natural language and markdown into native agent tool calls with **real-time thinking streams (`<thought>`)**, AST JSON self-healing, and keepalive pulses.
- **Full Developer MCP Suite (7 Specialized Tools)**: Wire your accounts into any MCP-capable IDE (Cursor, Windsurf, VS Code, Zed, Claude Desktop) with tools like `amux_ask`, `amux_review`, `amux_diagnose`, `amux_fix`, and `amux_analyze`.
- **Zero-Telemetry & Encrypted Secret Vault**: 100% offline and private. Secrets are encrypted locally via AES-256-GCM in macOS Keychain or an isolated keyfile (`~/.amux/master.key`).

---

## 📐 Architecture Overview

```
                      ┌───────────────────────────────────────┐
                      │            HUMAN DEVELOPER            │
                      └───────────────────┬───────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼ [Native Mode: direct binary]                  ▼ [Sandbox Mode: amux run <ide>]
   ┌─────────────────────────────────────────┐     ┌─────────────────────────────────────────┐
   │        DIRECT NATIVE EXECUTION          │     │        AMUX UNIVERSAL AI GATEWAY        │
   │  • Reads active token from Keychain     │     │  • Local HTTP Proxy on :8787            │
   │  • Direct upstream connection (0ms)     │     │  • Child-process isolated environment   │
   │  • Zero background proxy requirement    │     │  • Shell rc & launchctl 100% pristine   │
   └────────────────────┬────────────────────┘     └────────────────────┬────────────────────┘
                        │                                               │
                        │                                               ▼
                        │                          ┌─────────────────────────────────────────┐
                        │                          │        WEBLOOP 2.0 TOOL ENGINE          │
                        │                          │  • Real-time Thinking Stream (SSE)      │
                        │                          │  • Lenient AST JSON Auto-Repair         │
                        │                          │  • Bracket Parsing [Tool call: ...]     │
                        │                          │  • Line-bounded file editing for AGY    │
                        │                          └────────────────────┬────────────────────┘
                        │                                               │
                        ▼                                               ▼
   ┌─────────────────────────────────────────────────────────────────────────────────────────┐
   │                               PROVIDERS & MODEL POOL                                    │
   │   Tier 1: Subscriptions       │   Tier 2: Web Accounts        │   Tier 3: Metered APIs  │
   │   (Claude, Codex, AGY OAuth)  │   (ChatGPT, Gemini, Claude)   │   (DeepSeek, Groq, Kimi)│
   └─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 📦 Installation

### Option 1: Quick Install (Recommended — No `sudo` required)

Installs the binary into `$HOME/.local/bin/amux` and automatically configures shell PATH:

```bash
curl -fsSL https://raw.githubusercontent.com/ninhlee99/amux/main/install.sh | sh
```

### Option 2: Build from Source (Go 1.22+)

```bash
git clone https://github.com/ninhlee99/amux.git && cd amux
go build -o amux .

# Install to user space (No sudo needed):
mkdir -p ~/.local/bin && cp amux ~/.local/bin/

# Verify installation:
amux help
```

---

## 🚀 3-Minute Quickstart

### Step 1: Connect your accounts

```bash
# Connect official subscriptions (browser OAuth):
amux login claude        # Log in Account 1 for Claude Code
amux login claude        # Log in Account 2 (Account 1 is safely preserved)

# Connect Web sessions (free web quota):
amux login chatgpt       # Connect ChatGPT Web session
amux login gemini-web    # Connect Gemini Web session

# (Optional) Connect metered API keys:
amux login groq --token gsk_...
```

### Step 2: Run your coding agent in Sandbox

Run any coding IDE or CLI through the AMUX Gateway without altering global environment variables:

```bash
amux run claude          # Launch Claude Code CLI in isolated sandbox
amux run cursor          # Launch Cursor IDE connected to Gateway :8787
amux run windsurf        # Launch Windsurf connected to Gateway :8787
amux run agy             # Launch Google Antigravity CLI in sandbox
amux run codex           # Launch OpenAI Codex CLI in sandbox
```

*When you exit the session, your terminal is 100% clean. Running `claude` directly executes natively with direct connection.*

### Step 3: Monitor in Interactive TUI Dashboard

```bash
amux dashboard
```

An interactive terminal dashboard displaying real-time quota gauges, active provider routing, response latencies, and pool controls.

---

## 🔄 Account Switching & Rotation Pool

### Manual Switching (Instant, No Re-authentication)

Switching accounts swaps Keychain tokens and local configuration files instantly without requiring a browser login:

```bash
amux account list          # List all saved accounts
amux switch claude:code:01 # Switch Claude Code to account 1
amux switch                # Open interactive numbered picker
```

```
ID                       EMAIL              TYPE  ACTIVE  POOL  USAGE   THRESHOLD  RESETS IN
claude:code:01           alice@company.com  sub   yes     no    12%     100.0%     4h10m
claude:code:02           alice@gmail.com    sub   -       no    0%      100.0%     unknown
chatgpt:web:01           personal@web       web   -       yes   --      95.0%      ready
```

### Automatic Rotation Pool

The pool defines accounts that AMUX may automatically rotate between when usage limits are hit:

```bash
amux pool                                    # View accounts in the rotation pool
amux pool add claude:code:01 claude:code:02  # Enable automatic rotation between them
amux pool remove claude:code:02              # Return to manual switching only
```

> [!IMPORTANT]
> **Strict Subscription Protection Rule:** Subscriptions (Claude Code, Codex, Antigravity) are **NEVER** added to the rotation pool automatically upon login. They only rotate if you explicitly run `amux pool add`. Web and API accounts are pooled by default.

---

## 🔌 Model Context Protocol (MCP) Integration

Expose your entire pool of AI providers as tools for any MCP-compatible agent (Cursor, Windsurf, Claude Desktop, VS Code, Zed, Cline):

```bash
amux mcp install all       # Auto-detect and register into all installed IDEs
amux mcp status            # Verify registration across clients
amux mcp uninstall         # Remove AMUX MCP cleanly from all clients
```

### Available MCP Tools Suite

| Tool | Purpose |
| :--- | :--- |
| `amux_ask` | Prompt any provider or model family (e.g., `gemini:web`, `chatgpt`, `claude:code`). |
| `amux_review` | Perform thorough code review using specialized reasoning models. |
| `amux_diagnose` | Analyze compiler errors, exceptions, or terminal logs. |
| `amux_fix` | Generate precise file diffs and drop-in code fixes. |
| `amux_analyze` | Evaluate software architecture, complexity, and performance tradeoffs. |
| `amux_providers` | Inspect active and fallback providers, health states, and quota. |
| `amux_status` | Query live gateway connectivity, active sessions, and rotation state. |

---

## 🛠️ Supported IDEs & Clients Matrix

| Target | Command | Injected Sandbox Variables | Notes |
| :--- | :--- | :--- | :--- |
| **Claude Code** | `amux run claude` | `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` | Zero keychain pollution; seamless OAuth token swap. |
| **Cursor IDE** | `amux run cursor` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `OPENAI_API_KEY` | Auto-detected from PATH or `/Applications/Cursor.app`. |
| **Windsurf** | `amux run windsurf` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `ANTHROPIC_BASE_URL` | Auto-detected from PATH or `/Applications/Windsurf.app`. |
| **Antigravity** | `amux run agy` | `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_BASE`, `GOOGLE_GENAI_BASE_URL` | Schema-aware line bounds & `AbsolutePath` resolution. |
| **Codex CLI** | `amux run codex` | `OPENAI_BASE_URL`, `OPENAI_API_BASE`, `OPENAI_API_KEY` | Fallback to direct credentials on pool exhaustion. |
| **Arbitrary Agent** | `amux run <cmd>` | All universal proxy environment variables | Supports Aider, OpenCode, Cline, etc. |

---

## 📖 Complete CLI Reference

### 1. Gateway Lifecycle & Sandbox
- `amux run <ide> [args...]` — Launch IDE in isolated sandbox environment.
- `amux start` — Start the background gateway daemon (`-f` for foreground).
- `amux stop` — Stop the background gateway.
- `amux restart` — Restart the gateway daemon.
- `amux off` — Stop gateway and restore all IDE configs to native direct mode.
- `amux status` — Display gateway, hook states, and active accounts.

### 2. Accounts & Rotation
- `amux login [provider]` — Connect an account (OAuth browser, Web, or API token).
- `amux account list` (`amux ls`) — List all configured accounts and usage percentages.
- `amux switch [id]` — Switch active account for an IDE.
- `amux pool [add|remove] <id>` — Manage automatic failover rotation pool.
- `amux account off|on <id>` — Temporarily disable or re-enable an account.
- `amux account remove <id>` — Delete an account from AMUX.
- `amux account health` — Check health status of saved credentials.

### 3. Diagnostics & Monitoring
- `amux dashboard` — Open interactive full-screen terminal UI.
- `amux top` — Live stream of gateway requests, token usage, and latencies.
- `amux doctor [--fix]` — Diagnose keychain, permissions, and clean legacy hooks.
- `amux usage [day|week|month]` — View token consumption metrics.

### 4. MCP Server Management
- `amux mcp install [target|all]` — Register AMUX MCP server in coding hosts.
- `amux mcp uninstall [target|all]` — Unregister AMUX MCP server.
- `amux mcp status` — Check MCP server registration status.

---

## 🔒 Security & Privacy Model

- **100% Local**: AMUX never phones home or uploads telemetry to external servers.
- **Encrypted Secret Storage**: Credentials in `~/.amux` are encrypted with AES-256-GCM.
  - Default: Master key securely stored in macOS Keychain.
  - File-based mode: Run `amux config secret-store file` to isolate keys into `~/.amux/master.key` without accessing the system Keychain.
- **Localhost Bound**: Gateway listens strictly on `127.0.0.1:8787` by default.

---

## 🧹 Clean Uninstallation

AMUX respects your system. You can cleanly remove AMUX without leaving orphaned files:

| Command | What is Removed | What is Kept |
| :--- | :--- | :--- |
| `amux off` | Unhooks all IDEs; stops gateway | Accounts, logins, MCP registrations |
| `amux mcp uninstall` | Removes AMUX entries from IDE configs | All other MCP servers |
| `amux uninstall` | Hooks, gateway, statuslines, MCP entries, binaries | `~/.amux` (saved accounts) |
| `amux uninstall --purge` | Everything above + `~/.amux` + Keychain keys | Pristine system |

---

## ❓ Troubleshooting

- **IDE fails immediately after stopping gateway**: The IDE might still have static hooks enabled. Run `amux start` or `amux off` to return to native mode.
- **Switching says login expired**: Run `amux login <provider>` once to refresh the session token.
- **Port 8787 already in use**: Check conflicting processes with `lsof -i :8787` or set custom port via `export AMUX_PORT=8788`.
- **Diagnostics check**: Run `amux doctor --fix` for automated health diagnosis and self-healing.

---

## 📄 License

MIT © [ninhlee99](https://github.com/ninhlee99)
