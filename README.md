# AMUX — AI Developer Infrastructure & Identity Gateway

[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)](https://github.com/ninhlee99/amux)
[![Security](https://img.shields.io/badge/security-AES--256--GCM-green.svg)](docs/security-model.md)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**AMUX** is an AI developer infrastructure and identity gateway. It lets you use your AI tools (Claude Code, Cursor, OpenAI Codex, Google Antigravity/Gemini) normally while managing identities, quotas, and execution securely behind the scenes.

```
AI Clients & IDEs
  (Claude Code, Cursor, Codex CLI, Antigravity/AGY, Gemini Web, ChatGPT Web)
                           │
                           ▼
                    AMUX AI Gateway
  ┌─────────────────────────────────────────────────────────────┐
  │  • Flat Identity Lifecycle & Instant Multi-Account Switch   │
  │  • Native OS Keychain Sync (Zero-reauth required)           │
  │  • Localhost Streaming Reverse Proxy (:8787)                │
  │  • Encrypted-at-Rest Secret Vault (AES-256-GCM)             │
  └──────────────────────────────┬──────────────────────────────┘
                                 │
                                 ▼
                     Native Tools & Upstreams
  (IDE Tools, MCP Servers, OS Keychain, Claude/OpenAI/Gemini APIs)
```

---

## 🧭 Why AMUX? (Frequently Asked by Developers)

### 1. "Why do I need this?"
If you use multiple AI coding tools or multiple subscriptions (e.g., personal Claude Code + company Claude Code + Google Antigravity), switching accounts normally requires logging out in the browser, losing active session state, or re-authenticating repeatedly. AMUX manages these identities as first-class objects on your machine, enabling **instant zero-touch switching** between accounts without browser popups or lost tokens.

### 2. "Why should I trust it with my credentials?"
AMUX enforces a strict, local-only trust model:
- **Zero unencrypted storage**: All tokens and session cookies in `~/.amux/` are sealed with **AES-256-GCM authenticated encryption** using a master key derived from the OS Keychain.
- **Localhost only**: The gateway binds exclusively to `127.0.0.1:8787`.
- **No telemetry exfiltration**: Your code, prompts, and tokens never leave your local machine.
👉 Read the complete [AMUX Security & Trust Model](docs/security-model.md).

### 3. "What happens when AMUX crashes or is stopped?"
**Your tools keep working natively.** In zero-touch mode, AMUX synchronizes credentials directly into the native OS Keychain (`Claude Code-credentials`, `antigravity-service`) and IDE config files. If AMUX is stopped, Claude Code and Codex continue running directly against upstream APIs without any dependency on the AMUX daemon.

---

## 📊 Implementation Status

To provide full transparency between current production features and future roadmap items, AMUX categorizes capabilities into three distinct tiers:

### ✅ Stable (Production Ready)
- **Flat Identity Model**: Unified configuration schema (`~/.amux/identities.json`) with strict 1-account-per-provider deduplication.
- **Native OS Keychain Sync**: Instant switching (`amux switch <id>`) between Claude Code, AGY, and Codex accounts with zero browser re-login.
- **Encrypted Vault at Rest**: `AMENC1:` envelope format with AES-256-GCM cipher and 0600 file permission enforcement.
- **Local Gateway Daemon**: Background proxy daemon on `127.0.0.1:8787` with lifecycle management (`start`, `stop`, `restart`, `status`).
- **Client IDE Hooks**: Transparent config patching and restoration for Claude Code, Cursor, and Codex.

### 🧪 Experimental (Active Evaluation)
- **Web Session Pool**: Cookie-based failover pool for ChatGPT Web and Gemini Web sessions.
- **Adaptive Quota Thresholds**: Auto-rotation triggers when an active identity hits its defined usage threshold (default: 95.0%).
- **Context Deduplication (`pkg/ctxshrink`)**: Prompt and response caching to reduce token retransmission overhead.

### 🚧 Planned Roadmap
- **Universal Tool IR**: Normalized intermediate representation for cross-tool translation between Anthropic `tool_use` and OpenAI `tool_calls`.
- **Cross-Dialect Multi-Provider Failover**: Dynamic automatic fallback routing across disparate model families during major cloud outages.
- **Cross-Machine Vault Sync**: End-to-end encrypted backup of identity configurations across developer workstations.

---

## 🚀 2-Minute Quickstart

### 1. Installation (macOS & Linux)
```bash
# Clone and build binary
git clone https://github.com/ninhlee99/amux.git
cd amux
go build -o amux .
sudo cp amux /usr/local/bin/amux
```

### 2. Authenticate Your Accounts
```bash
amux login claude       # Claude Code OAuth PKCE or browser login
amux login codex        # OpenAI Codex CLI authentication
amux login agy          # Google Antigravity / Google account snapshot
amux login chatgpt      # ChatGPT Web session pool
amux login api          # Custom OpenAI-compatible endpoints (DeepSeek, Ollama)
```

### 3. Start the Gateway Daemon
```bash
amux start              # Starts background daemon on http://127.0.0.1:8787
```

### 4. Direct Your IDE to the Gateway (Optional)
```bash
amux hook --all         # Automatically hooks detected IDEs (Claude Code, Cursor, Codex)
```
*You can now run your tools natively (e.g. `claude` or `codex`). AMUX manages accounts and quotas in the background.*

---

## 🛠️ CLI Command Reference

### Identity & Account Management
| Command | Description |
| :--- | :--- |
| `amux login [provider]` | Interactive or direct authentication for a provider |
| `amux id list` (or `amux account`) | Display all identities, models, quotas, active state & auto-switch |
| `amux switch <id>` | Instantly switch active account (updates OS Keychain & IDE configs) |
| `amux id remove <id>` | Remove identity and safely purge its credentials |
| `amux id enable <id>` | Re-enable an identity in the failover rotation pool |
| `amux id disable <id>` | Temporarily disable an identity from failover rotation |
| `amux id auto <id> [on\|off]` | Toggle auto-failover eligibility |
| `amux id threshold [id] [val]` | Get or set account failover quota threshold (default: 95.0%) |

### Gateway Daemon Management
| Command | Description |
| :--- | :--- |
| `amux start [-f]` | Start gateway daemon (`-f` foreground mode) |
| `amux stop` | Gracefully stop background daemon |
| `amux restart` | Stop and restart background daemon |
| `amux status` | Real-time status dashboard of identities, active hooks, and daemon |
| `amux hook [flags]` | Hook IDE configurations (`--claude`, `--cursor`, `--codex`, `--agy`, `--all`) |
| `amux unhook [flags]` | Restore IDE configurations to direct upstream connections |

### Diagnostics & Security
| Command | Description |
| :--- | :--- |
| `amux doctor` | Deep diagnostics: network, keychain access, tools & daemon health |
| `amux usage [day\|week\|month]` | Token usage analytics, cost estimates & request history |
| `amux config [property]` | Inspect or modify configuration properties |
| `amux migrate` | Non-destructive migration from legacy accounts to flat Identity model |
| `amux uninstall [--purge]` | Uninstall AMUX binary and hooks (`--purge` wipes `~/.amux`) |

---

## 🔄 Multi-Account Switching Workflow

When working with multiple subscriptions (e.g. personal and company Claude Code accounts):

```bash
# 1. List all configured identities
$ amux id list
ID                   EMAIL                      MODEL              THRESHOLD      USAGE    ACTIVE   AUTO-SWITCH  RESETS IN
claude:code:01       alice@company.com          claude-3-7-sonnet  90.0%          12.4%    YES *    ON           unknown
claude:code:02       alice.personal@gmail.com   claude-3-7-sonnet  90.0%          0.0%     NO       ON           unknown

# 2. Switch to personal account instantly
$ amux switch claude:code:02
✓ Current active account credentials snapshotted
✓ Native credentials updated for anthropic execution
✓ Account "claude:code:02" is now ACTIVE.
```
**Zero browser re-login is required.** Tokens are preserved and loaded directly into the native runtime.

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for more information.
