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
amux id add claude    # OAuth, CLI snapshot, or browser login
amux id add codex     # OpenAI Codex CLI or standalone OAuth
```

### 4. Inspect Runtime & Quotas
```bash
amux status
```

---

## 🛠️ Minimal Command Surface

### 1. Setup & Diagnostics
```bash
amux setup           # Guided setup wizard
amux status          # Real-time dashboard of identities, quotas, and gateway state
amux doctor          # Probes OS Keychain access, upstream network, tools, and daemon
```

### 2. Identity Management (`amux id`)
```bash
amux id list         # Display flat accounts, tier, credentials status, and usage %
amux id add [tool]   # Add identity (OAuth PKCE, API key, local session, or CDP)
amux id select [id]  # Manual account switch (interactive picker; updates OS Keychain)
amux id health       # Probe token lifetimes and quota limits
amux id remove <id>  # Delete identity from persistence
```

### 3. Universal Gateway (`amux gateway`)
```bash
amux gateway start   # Start detached background gateway service (:8787)
amux gateway stop    # Gracefully stop the gateway daemon
amux gateway status  # Inspect socket status and active IDE hooks

# Manual IDE hook overrides:
amux gateway hook --claude
amux gateway hook --cursor
amux gateway hook --codex
amux gateway hook --all
amux gateway unhook [--claude|--cursor|--codex|--all]
```

### 4. Configuration & Migration
```bash
amux config                        # Inspect active JSON configuration
amux config threshold <percent>    # Update multi-account failover threshold (default: 95.0)
amux migrate                       # Non-destructive auto-migration from legacy accounts
```

### 5. System
```bash
amux update [--force]   # In-place update to latest GitHub release
amux uninstall [--purge] # Remove hooks and binaries (--purge also wipes ~/.am)
```

---

## 🧰 The Universal Tool Engine

Tools and function calls never undergo point-to-point translations. All requests and responses pass through the **Canonical Tool Model**:
```
Inbound Wire -> Tool Adapter -> Canonical UniversalTool IR -> Security Layer -> Adapter -> Target Wire
```

### Canonical Intermediate Representation
- **`UniversalTool`**: ID, Name, Description, InputSchema (`map[string]interface{}`), Source, Metadata.
- **`UniversalToolCall`**: ID, Name, Arguments (`map[string]interface{}`), RawJSON.
- **`UniversalToolResult`**: ToolCallID, Success, Output string, Error (*UniversalToolError), Metadata.

### Supported Bidirectional Formats
- **Anthropic Claude**: `tool_use` (id, name, input) ↔ `tool_result` (tool_use_id, content, is_error).
- **OpenAI Function Calling**: `tool_calls` (id, type: "function", name, arguments) ↔ `role: "tool"`.
- **Google Gemini**: `functionCall` (name, args) ↔ `functionResponse` (name, response).
- **Model Context Protocol (MCP)**: `tools/list` manifests ↔ `tools/call` invocations and results.
- **Agent Text-Loop**: Detects and adapts `<tool_call>{"name": "...", "arguments": {...}}</tool_call>`.

### 100% JSON Schema Constraint Preservation
The engine strictly guarantees that JSON schema constraints (`properties`, `required`, `enum`, `items`, `oneOf`, `anyOf`, `default`, `description`, `additionalProperties`) survive conversion 100% intact.

### Universal Web-Loop Tool Emulation
For Web Session accounts lacking native API function calling:
1. Serializes `UniversalTool` definitions into system instructions with strict format specifications.
2. Intercepts streaming chunks matching the ````tool_call` delimiter.
3. Emits native client tool events (`content_block_start` for Claude, `delta.tool_calls` for OpenAI).
4. Injects client `tool_result` back as user turns for uninterrupted multi-turn execution.

---

## 🛡️ Protected Syntax & Targeted Privacy Isolation

### Protected Patterns
AMUX enforces zero mutation, zero sanitization, and zero truncation for:
- **XML Boundaries**: `<tools>`, `<merchant_data>`, `<context>`, `<thinking>`, `<function_calls>`, etc.
- **Domain Identifiers**: Shopify global GraphQL IDs (`gid://shopify/<Resource>/<Id>`).
- **Version Control Markers**: Git commit SHAs (40-char & 7-char hex), cryptographic hashes (`sha256:`, `md5:`), and Unified Diff Headers (`--- a/...`, `+++ b/...`, `@@ ... @@`).
- **Terminal Fences**: ````bash````, ````shell````, ````tool_call````, and inline signatures (`Bash(command="...")`).

### Targeted Isolation Redactor
`pkg/privacy/redact.go` scans **ONLY**:
1. Authorization headers (`Authorization: Bearer ...`, `x-api-key: ...`, `Cookie: session=...`).
2. Standalone API keys with known provider prefixes (`sk-ant-`, `sk-proj-`, `sk-`, `AIzaSy`, `ghp_`).
Broad regexes across message bodies are strictly excluded, eliminating payload corruption.

---

## 🏗️ Architecture Directory Structure

```
amux/
├── cmd/
│   └── amux/                 # Minimal binary entry point (< 25 lines)
├── pkg/
│   ├── cli/                  # Decomposed single-responsibility CLI modules (< 65 lines dispatcher)
│   ├── identity/             # Flat Identity model, OS Keychain rotation & threshold engine
│   ├── gateway/              # Universal AI Gateway, 1:1 passthrough & dynamic IDE hooking
│   ├── router/               # Quota-aware routing, session affinity & tier ordering
│   ├── tools/                # Universal Tool Engine, canonical IR & bidirectional adapters
│   ├── context/              # Context memory & safe compaction (active only on mid-session rotation)
│   ├── agent/                # Multi-agent coordination & delegation lifecycle
│   ├── telemetry/            # Millisecond-precision structured logging & metrics
│   ├── privacy/              # Isolated header & credential redactor
│   ├── usage/                # Accurate token capture & rolling quota calculation
│   └── ui/                   # Terminal dashboard & interactive selectors
```

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
- **Passthrough Fidelity**: 1:1 bitwise streaming verification for matching dialects.
