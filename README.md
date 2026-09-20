# AMUX — Thin AI Gateway & AI Developer Infrastructure

[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)](https://github.com/ninhlee99/amux)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**AMUX** is a high-performance, thin AI Gateway and developer runtime infrastructure layer. It connects any AI client interface (Claude Code, Cursor, Codex CLI, Antigravity/AGY, ChatGPT Web, Claude Web, Custom Agents) to native execution capabilities (IDE tools, MCP tools, filesystem, terminal) through dynamic capability discovery, strict host-aware contracts, and seamless multi-account lifecycle management.

```
AI Clients & Interfaces
  (Claude Code, Cursor, Codex CLI, AGY, ChatGPT Web, Claude Web, Custom Agents)
                           │
                           ▼
                    AMUX AI Gateway
  ┌─────────────────────────────────────────────────────────────┐
  │  • 1:1 Bitwise Streaming Passthrough (Zero-deserialization) │
  │  • Host-Aware Capability Contracts (No tool hallucinations) │
  │  • Silent OS Keychain Rotation & Quota Failover Engine      │
  │  • Dynamic Tool Registry & Native Execution Engine          │
  └──────────────────────────────┬──────────────────────────────┘
                                 │
                                 ▼
                     Native Capabilities & Upstreams
  (Native IDE Tools, MCP Servers, OS Keychain, Filesystem, Terminal, APIs)
```

---

## ⚡ Core Architectural Principles

1. **Thin Gateway, Not an Agent Framework**:
   - AMUX is an AI Gateway and execution bridge, **not** an agent runtime or workflow engine.
   - It connects AI clients to native capabilities without heavy abstractions or fragile point-to-point translation layers.

2. **Host-Aware Capability Contracts & Zero Tool Hallucination**:
   - Every client communicates in its native IDE tool format (Claude Code tools, Codex functions, Gemini declarations, or MCP tools).
   - Strict gateway contracts explicitly forbid models from hallucinating unsupported or cross-IDE tools.

3. **Zero-Touch OS Keychain & Multi-Account Lifecycle**:
   - **Scenario `Login A → Login B → Switch A` requires ZERO re-authentication.**
   - AMUX safely snapshots active credentials and rotates native macOS Keychain entries (`Claude Code-credentials`, `gemini`, etc.) and IDE configuration files on demand.

4. **Automated Quota Failover (95% Multi-Account / 100% Single-Account)**:
   - When an active subscription reaches `threshold_pct` (default: 95.0%), AMUX silently rotates native credentials to the next available healthy subscription in the pool.
   - Single-account setups run up to 100.0% capacity before fallback triggers.

5. **1:1 Bitwise Passthrough**:
   - Matching dialects (Claude Code → Anthropic API, Codex → OpenAI API) operate as transparent reverse proxies, preserving exact Server-Sent Event (SSE) chunk boundaries, headers, multi-turn messages, and custom tool parameters without unnecessary JSON round-tripping.

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
amux login antigravity  # Antigravity / Google account snapshot
amux login chatgpt      # ChatGPT Web session pool
amux login api          # Custom OpenAI-compatible endpoints (DeepSeek, Ollama, vLLM)
```

### 3. Start the AMUX Gateway Daemon
```bash
amux start              # Starts background daemon on http://127.0.0.1:8787
```

### 4. Direct Your IDE / Tools to the Gateway
```bash
amux hook --all         # Automatically hooks detected IDEs (Claude Code, Cursor, Codex)
```
*Run your tools natively! For example, run `claude` or `codex` — AMUX silently provides multi-account failover, quota pooling, and capability management in the background.*

---

## 🛠️ CLI Command Reference

### Gateway Daemon Management
| Command | Description |
| :--- | :--- |
| `amux start [-f] [-p]` | Start gateway daemon (`-f` foreground, `-p` public `0.0.0.0:8787`) |
| `amux stop` | Gracefully stop background daemon |
| `amux restart` | Stop and restart background daemon |
| `amux status` | Real-time dashboard of accounts, active hooks, quotas & daemon state |
| `amux hook [flags]` | Hook IDE configurations (`--claude`, `--cursor`, `--codex`, `--agy`, `--all`) |
| `amux unhook [flags]` | Restore IDE configurations to direct upstream connections |

### Account Lifecycle Management
| Command | Description |
| :--- | :--- |
| `amux login [provider]` | Interactive or direct authentication for a provider |
| `amux account list` (or `amux account`) | Display all accounts, emails, quotas, active state & auto-switch |
| `amux switch <id>` | Instantly switch active account (updates OS Keychain & IDE configs) |
| `amux account logout <id>` | Remove account and safely purge its credentials |
| `amux account on <id>` | Re-enable an account in failover rotation pool |
| `amux account off <id>` | Temporarily disable an account from failover rotation |
| `amux account auto <id> [on\|off]` | Toggle auto-failover eligibility (set `off` for manual-only accounts) |
| `amux account threshold [id] [val]` | Get or set account failover quota threshold (default: 95.0%) |
| `amux account health` | Probe token validity and quota limits across all configured accounts |

### Diagnostics & Configuration
| Command | Description |
| :--- | :--- |
| `amux doctor` | Deep system diagnostics: network, keychain access, tools & daemon socket |
| `amux usage [day\|week\|month]` | Token usage analytics, cost estimates & request history |
| `amux setup` | Guided interactive setup wizard |
| `amux config [property]` | Inspect or modify configuration values |
| `amux migrate` | Non-destructive migration to flat Identity model |
| `amux update [--force]` | In-place update to latest GitHub release |
| `amux uninstall [--purge]` | Uninstall AMUX binary and hooks (`--purge` wipes `~/.amux`) |

---

## 🔄 Account Lifecycle & Failover Workflow

### Switching Between Multiple Accounts
When working with multiple subscriptions (e.g. personal and company Claude Code accounts):

```bash
# List all accounts
$ amux account list
ID                   EMAIL                      MODEL              THRESHOLD      USAGE    ACTIVE   AUTO-SWITCH  RESETS IN
claude:code:01       alice@company.com          claude-3-7-sonnet  90.0%          12.4%    YES      ON           unknown
claude:code:02       alice.personal@gmail.com   claude-3-7-sonnet  90.0%          0.0%     NO       ON           unknown

# Switch to personal account
$ amux switch claude:code:02
✓ Current active account credentials snapshotted
✓ Native credentials updated for anthropic execution
✓ Account "claude:code:02" is now ACTIVE.
```
**No browser re-login is ever required.** Your single-use refresh tokens and session states are safely preserved.

### Silent Quota Failover
When an active subscription hits its quota threshold:
1. **Detection**: AMUX detects rate limit or capacity warning (`usage >= threshold_pct`).
2. **Keychain Rotation**: AMUX locates the next healthy subscription in the pool and silently updates the native OS Keychain.
3. **Transparent Continuation**: The developer's CLI or IDE continues running seamlessly.
4. **Gateway Fallback**: If all subscriptions for a provider are exhausted, the gateway routes overflow traffic to web session pools or metered API keys as a safety net.

---

## 🧰 Native Capability Registry & Runtime Engine

AMUX features an integrated **Capability Registry** (`pkg/runtime/`):

- **Dynamic Manifest Discovery**: Discovers native tools from active IDE environments and local MCP configuration (`~/.gemini/antigravity-cli/mcp`, `~/.claude/mcp.json`).
- **Host-Aware Contracts**: Generates precise system contracts tailored to the client interface:
  - *Claude Code*: Native `tool_use` blocks with ephemeral prompt caching.
  - *Cursor / Codex*: Standard `tool_calls` with function schemas.
  - *Gemini / AGY*: Structured `functionDeclarations`.
  - *MCP Protocol*: Standard JSON-RPC 2.0 `tools/list` and `tools/call`.
- **Native Execution Runtime**: Safely executes commands, filesystem operations, and MCP queries with process isolation, timeout enforcement, working directory confinement, and structured telemetry.

---

## 🛡️ Security & Privacy Architecture

- **OS Keychain Vault**: Credentials never sit unencrypted on disk. macOS Keychain and platform-native secret vaults store tokens.
- **Local-Only Gateway by Default**: Gateway binds strictly to loopback (`127.0.0.1:8787`).
- **Optional Public Mode**: When started with `--public`, access requires an ephemeral 24-byte bearer token (`amux-...`) verified with constant-time comparison, backed by IP rate limiting against brute-force attacks.
- **Secret Isolation**: AMUX guarantees that authorization headers, API keys, and sensitive tokens are never echoed in logs or error traces.

---

## ❓ Troubleshooting & FAQ

### Port 8787 is already in use
```bash
# Check running gateway or conflicting process
amux status
# Or stop the conflicting process and restart
amux restart
```

### Claude Code reports expired token after machine sleep
```bash
# Run doctor to verify keychain tokens
amux doctor
# Or force refresh via account switch
amux switch claude:code:01
```

### Checking Daemon Logs
Logs and execution traces are stored in `~/.amux/`:
```bash
tail -f ~/.amux/amux.log
```

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for more information.
