# AMUX — AI Development Runtime & Universal AI Gateway

AMUX is an AI-native development infrastructure layer for modern engineering environments:

```
Human Developer
      ↓
AI Applications (Claude Code, Codex CLI, Cursor, Gemini/AGY, Autonomous Agents)
      ↓
AMUX Runtime Layer (Identity Layer, Context Layer, Universal AI Gateway, Universal Tool Engine)
      ↓
Providers & Models (Anthropic, OpenAI, Gemini, Local, Custom Endpoints)
```

---

## ⚡ Core Architectural Principles

1. **Zero-Touch & Non-Invasive by Default**:
   - AMUX operates strictly detached in the background.
   - It never mutates shell configuration (`.zshrc`, `.bashrc`) or exports global proxy environment variables.
   - Client IDEs (`claude`, `codex`, `cursor`) communicate directly with official upstreams using credentials loaded natively from the system OS Keychain.

2. **Silent Keychain Rotation & Conditional Gateway Injection**:
   - **Threshold Rules**:
     - *Multi-Account Pool*: Failover triggers when the active subscription reaches `threshold_pct` (default: 95.0%).
     - *Single-Account Pool*: A provider pool with exactly 1 subscription account is permitted to reach 100.0% capacity before fallback triggers.
   - **Silent Keychain Rotation**: When Subscription Account $N$ hits its threshold, AMUX silently updates the OS Keychain credential entry (`Claude Code-credentials`, etc.) with Subscription Account $N+1$. The IDE continues native upstream execution without gateway interception.
   - **Conditional Gateway Injection**: ONLY when **ALL** subscription accounts of a provider reach their threshold does AMUX hook the gateway URL (`http://127.0.0.1:8787`) into the IDE settings.
   - **Auto-Detachment**: As soon as any subscription account resets its quota (`usage_percent < threshold_pct`), AMUX automatically detaches the gateway hook from the IDE settings and restores direct native Keychain execution.

3. **Absolute Account Tier Parity**:
   - Subscriptions (OAuth), Web Sessions (Cookies/CDP), and Metered API Keys are first-class peers with full support for multi-turn conversations and tool execution.
   - Routing priority is governed strictly by **Availability & Cost** (never by whether a request contains tools):
     1. Active Subscription (maximize fixed cost).
     2. Free / Web Session (leverage free quota upon subscription exhaustion).
     3. Metered API Key (last-resort safety net).

4. **Transparent 1:1 Bitwise Passthrough**:
   - When `ClientDialect == TargetDialect` (e.g., Claude Code -> Anthropic API, Codex CLI -> OpenAI API), the gateway operates as a pure transparent streaming reverse proxy.
   - Bypasses intermediate JSON deserialization, streaming raw byte streams verbatim to preserve exact Server-Sent Event (SSE) chunk boundaries, headers, multi-turn messages, custom tool parameters, and tool results.

5. **No Application Launcher Responsibility**:
   - AMUX never launches client applications (`amux run <app>` is eliminated). Developers run `claude`, `codex`, `cursor` natively.

---

## 🚀 Quickstart

### 1. Installation (macOS)
```bash
curl -fsSL https://raw.githubusercontent.com/ninhlee99/amux/main/install.sh | sh
```
*Builds the single binary `amux` into `~/.local/bin` (or `/usr/local/bin`).*

### 2. Guided Setup
```bash
amux setup
```

### 3. Add Identities
```bash
amux id add claude    # OAuth, CLI snapshot, or CDP browser login
amux id add codex     # OpenAI Codex CLI or standalone OAuth
amux id add custom    # Any OpenAI-compatible endpoint (Ollama, vLLM, DeepSeek, Together)
```

### 4. Inspect Runtime & Quotas
```bash
amux status
```

---

## 🛠️ Minimal Command Surface

### 1. Setup & Diagnostics
```bash
amux setup                          # Guided setup wizard
amux status                         # Real-time dashboard of identities, quotas, and gateway state
amux usage day [YYYY-MM-DD]         # Per-account breakdown for a specific date
amux usage week [YYYY-MM-DD]        # Daily token breakdown for all accounts across the week
amux usage month [YYYY-MM]          # Weekly summary & daily breakdown for the month
amux doctor                         # Probes OS Keychain access, upstream network, tools, and daemon
```

### 2. Identity Management (`amux id`)
```bash
amux id list                        # Display flat accounts, tier, credentials status, usage % & auto-switch
amux id add [provider]              # Add identity: claude, codex, antigravity, or custom
amux id select [id]                 # Manual account switch (interactive picker; updates OS Keychain)
amux id auto <id> [on|off]          # Toggle auto-rotation (off = MANUAL ONLY; excluded from auto-switch)
amux id health                      # Probe token lifetimes and quota limits
amux id remove <id>                 # Delete identity from persistence
```

### 3. Universal Gateway (`amux gateway`)
```bash
amux gateway start [-d] [--public]  # Start gateway service (:8787 or 0.0.0.0:8787 in public mode)
amux gateway stop [--public]        # Stop gateway daemon
amux gateway status                 # Inspect socket status, active IDE hooks, and public mode status
amux gateway token [new|clear]      # Manage gateway bearer access token for external/remote clients

# IDE Hook Injection (Routes IDE requests through gateway for live failover & switch):
amux gateway hook claude            # Hook Claude Code (sets ANTHROPIC_BASE_URL in ~/.claude/settings.json)
amux gateway hook cursor            # Hook Cursor IDE
amux gateway hook codex             # Hook Codex CLI
amux gateway hook all               # Hook all detected IDEs
amux gateway unhook [target]        # Restore direct native execution (claude, cursor, codex, all)
```

### 4. Configuration & Migration
```bash
amux config                         # Inspect active JSON configuration
amux config threshold <percent>     # Update multi-account failover threshold (default: 95.0)
amux migrate                        # Non-destructive auto-migration from legacy accounts
```

### 5. System
```bash
amux update [--force]               # In-place update to latest GitHub release
amux uninstall [--purge]            # Remove hooks and binaries (--purge also wipes ~/.amux)
```

---

## 🧰 The Universal Tool Engine & Protocols

Tools and function calls never undergo fragile point-to-point conversions. All requests and responses pass through the **Canonical Tool Model**:

```
Inbound Wire Format
       ↓
Input Dialect Adapter (Anthropic / OpenAI / Gemini / MCP)
       ↓
Canonical UniversalTool IR (100% JSON Schema Constraint Preserved)
       ↓
Security & Privacy Layer (Protected XML & Secret Isolation)
       ↓
Output Dialect Adapter / Web-Loop Emulator
       ↓
Target Wire Format
```

### 1. Canonical Intermediate Representation (IR)
- **`UniversalTool`**: ID, Name, Description, InputSchema (`map[string]interface{}`), Source, Metadata.
- **`UniversalToolCall`**: ID, Name, Arguments (`map[string]interface{}`), RawJSON.
- **`UniversalToolResult`**: ToolCallID, Success, Output string, Error (`*UniversalToolError`), Metadata.
- **`UniversalToolError`**: Code, Message, Retryable, Provider.

### 2. Supported Tool Formats & Dialects
- **Anthropic Claude Code (`DialectClaude`)**:
  - Declaration: `tools[]` with `name`, `description`, `input_schema`, and `cache_control: {"type": "ephemeral"}`.
  - Invocations: Content block `type: "tool_use"` (`id`, `name`, `input`).
  - Responses: Content block `type: "tool_result"` (`tool_use_id`, `content`, `is_error`).
  - CoT Preservation: Preserves Anthropic `thinking` blocks (`budget_tokens`, `signature`) across turns.
- **OpenAI / Cursor / Codex CLI (`DialectCursor`, `DialectCodex`)**:
  - Declaration: `tools[]` with `type: "function"`, `function: {name, description, parameters}`.
  - Invocations: `tool_calls[]` (`id`, `type: "function"`, `function: {name, arguments: string}`).
  - Responses: Message with `role: "tool"`, `tool_call_id`, `content`.
  - CoT Preservation: Reasoning effort (`low`, `medium`, `high`) for o1 / o3-mini / Codex.
- **Google Gemini / Antigravity (`DialectGemini`)**:
  - Declaration: `functionDeclarations[]` with `name`, `description`, `parameters`.
  - Invocations: `functionCall` (`name`, `args`).
  - Responses: `functionResponse` (`name`, `response`).
  - CoT Preservation: Gemini Thought Signatures (`thought: true`, `parts: [{thought: "..."}]`).

### 3. Model Context Protocol (MCP) Support
- Fully compliant with the Anthropic Model Context Protocol specification:
  - Manifest discovery: `tools/list` returns schema definitions.
  - Tool invocation: `tools/call` with `params: {name, arguments}`.
  - Result schema: `mcpResult` with `content: [{type: "text", text}], isError`.
- **Automatic Namespacing Resolution**: Intelligently normalizes namespaced MCP tools (e.g. `mcp__server__tool`, `mcp_server_tool`, `server_tool`) across all client IDEs and backend providers.

### 4. Claude Code Skills & Protected Syntax (`pkg/tools/protect.go`)
- **Skill Tool Preservation**: The Claude Code `Skill` tool (e.g. `Skill(skill="review")`, `load_skill`, `run_skill`) is recognized and mapped bidirectionally.
- **Protected Tags**: AMUX guarantees that syntax markers including `<skills>`, `<available_skills>`, `<skill_definition>`, `<thinking>`, and `<context>` are **never truncated, redacted, or mutated**.
- **Privacy Redactor**: Targets strictly secret authorization headers (`Authorization: Bearer ...`, `x-api-key: ...`, `Cookie: session=...`) and standalone API keys (`sk-ant-`, `sk-proj-`, `AIzaSy`). Code diffs, file trees, and skill definitions remain 100% intact.

### 5. Universal Web-Loop Tool Emulation (`webloop.go`)
For Web Session accounts (Claude Web, ChatGPT Web, Gemini Web) lacking native API function calling:
1. Serializes `UniversalTool` definitions into the system prompt with strict schema specifications.
2. Intercepts streaming chunks matching `<tool_call>...</tool_call>` or ````tool_call ...````.
3. Emits native client tool events (`content_block_start` for Claude, `delta.tool_calls` for OpenAI).
4. Injects client `tool_result` back as user turns for uninterrupted multi-turn loops.

---

## 🛡️ Public Gateway Security & Rate Limiting

When running in public mode (`amux gateway start --public`):
- **Ephemeral Bearer Token**: Generates a 24-byte cryptographically random token `amux-<48 hex chars>` persisted at `~/.amux/proxy.token` (`0600`).
- **Zero-Touch Local Bypass**: Loopback traffic (`127.0.0.1`, `::1`) bypasses auth challenges so local developer CLI workflows never break.
- **Constant-Time Verification**: Remote requests require `Authorization: Bearer amux-...`, verified via `subtle.ConstantTimeCompare` to prevent timing attacks.
- **Anti-Brute-Force Rate Limiting**: Max **10 failed authentication attempts per minute**. Violating IPs receive `HTTP 429 Too Many Requests`.
- **Memory DoS Protection**: Bounded sliding-window tracking (max 50,000 IPs, 20 timestamps per IP) with automatic expired entry eviction.

---

## ⚖️ Architectural Comparison: AMUX vs Other Open-Source Tools

| Dimension | LiteLLM / LiteLLM Proxy | One-API / New-API | Ad-hoc CLI Proxies | **AMUX** |
| :--- | :--- | :--- | :--- | :--- |
| **Primary Architecture** | Cloud API Gateway / Proxy | Multi-tenant Token Reseller | Shell Wrapper / Hook Hack | **AI Development Runtime & Universal Gateway** |
| **Zero-Touch OS Keychain** | ❌ None (Requires base URL) | ❌ None (Manual API keys) | ❌ None (Overwrites `.zshrc`) | ✅ **Silent macOS Keychain Swapping** |
| **Account Tier Parity** | ❌ API Keys only | ❌ API Keys only | ⚠️ Single Account / Flaky | ✅ **Subscription (OAuth) → Web (CDP) → API Key** |
| **Web Account Support** | ❌ No browser/web support | ❌ No browser/web support | ⚠️ Fragile session cookies | ✅ **Headless Chromium CDP + Cookie Auto-sync** |
| **Tool Calling Matrix** | ⚠️ OpenAI format biased | ⚠️ Partial API translation | ❌ Passthrough only (Breaks) | ✅ **Canonical IR (Claude ↔ OpenAI ↔ Gemini ↔ MCP)** |
| **MCP Protocol Support** | ❌ No native MCP mapping | ❌ None | ❌ None | ✅ **Bidirectional MCP (`tools/list`, `tools/call`)** |
| **Prompt Cache Protection**| ❌ Unaware of prompt cache | ❌ Incompatible | ❌ Re-hashes entire history | ✅ **Preserves Cache Prefix (Up to 90% Cost Saving)** |
| **Conditional Hooking** | ❌ Always proxies | ❌ Always proxies | ⚠️ Permanent environment var | ✅ **Hooks only when ALL subs hit 95%; Auto-detaches** |
| **Footprint & Runtime** | Heavy Python / Node service | Heavy Go + MySQL + Redis | Bash / Python scripts | ✅ **Single Native Go Binary (<25MB, 0 DB)** |

---

## 🧪 Verification & Test Matrix

Run the test suite across all subsystems:
```bash
go test -v ./...
```
Verification includes:
- **Cross-Dialect Matrix**: Claude ↔ OpenAI, Claude ↔ Gemini, MCP ↔ Claude, Web Emulation ↔ Client Tool Calls.
- **Payload Integrity**: `<merchant_data>`, `gid://shopify/...`, git diffs, and terminal invocations round-trip verification.
- **Keychain Rotation**: Multi-account 95% threshold failover and single-account 100% capacity rule.
- **Auto-Switch OFF Exclusion**: Strict exclusion of manual-only accounts across Subscription and Web tiers.
- **Passthrough Fidelity**: 1:1 bitwise streaming verification for matching dialects.
