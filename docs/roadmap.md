# AMUX 30/60/90 Day Refactoring & Evolution Roadmap

This document outlines the strategic engineering roadmap for AMUX. It classifies all features and development tracks into **Stable**, **Experimental**, and **Planned** tiers, establishing clear delivery milestones across 30, 60, and 90-day horizons.

---

## 1. Feature Classification Matrix

To ensure stability for developers while aggressively iterating on AI-native tooling, AMUX divides capabilities into three tiers:

```text
┌────────────────────────────────────────────────────────────────────────┐
│  🟢 STABLE (Production-Ready)                                          │
│  • Flat Identity Store & AES-256-GCM Vault                             │
│  • macOS Keychain / Linux Secret Service Master Key Binding            │
│  • Domain-Based CLI Routing (Identity, Gateway, Diagnostics)           │
│  • Zero-Touch Native Tool Execution (Claude Code, Codex, Cursor, AGY)  │
│  • 1-Account-per-Provider Hierarchy & Deduplication                    │
│  • Targeted Secret Redaction & Bitwise Log Protection                  │
├────────────────────────────────────────────────────────────────────────┤
│  🟡 EXPERIMENTAL (Opt-in / Preview)                                    │
│  • Localhost Streaming Gateway (127.0.0.1:8787 Reverse Proxy)          │
│  • Dynamic IDE Hooking & Conditional Auto-Detach (Quota Thresholds)     │
│  • Web-Loop Emulation Harness & Browser Session Bridge                 │
│  • Cross-Dialect Universal Tool Converter (Anthropic ⇄ OpenAI ⇄ Gemini) │
│  • Rolling 5-Hour / 7-Day Sliding Window Quota Trackers               │
├────────────────────────────────────────────────────────────────────────┤
│  🔵 PLANNED (Research & Design Phase)                                  │
│  • Universal Model Context Protocol (MCP) Native Broker                │
│  • Multi-Agent Delegation & Orchestration Layer (`pkg/agent`)          │
│  • Project Context Graph & Shared Memory Synchronizer (`pkg/context`)   │
│  • Local Model Runtime Bridge (Ollama / vLLM / llama.cpp)              │
│  • Team Secret Sync & Enterprise RBAC Vault Federation                │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 30-Day Milestone: Architectural Foundations & Trust Hardening

**Primary Objective**: Eliminate architectural debt, remove monolithic dependencies, and solidify trust with hardware-grade security and zero-touch developer workflows.

### Deliverables & Task Breakdown:
- [x] **CLI Domain-Based Router (`pkg/cli/router.go`)** [🟢 Stable]
  - Decompose monolithic `cli.go` into domain handlers: Identity, Gateway, and Diagnostics.
  - Enforce strict help flag propagation and input validation.
  - Completely deprecate invasive `amux run <app>` launcher and legacy aliases.
- [x] **Hardware-Bound AES-256-GCM Vault (`AMENC1:`)** [🟢 Stable]
  - Integrate OS Keyring master key derivation (macOS Keychain via Security framework, Linux Secret Service via D-Bus).
  - Headless/CI fallback using PBKDF2 (HMAC-SHA256, 100k rounds, machine UUID salt).
  - Atomic, tamper-resistant vault writes with POSIX `0600`/`0700` permission enforcement.
- [x] **Targeted Privacy Redactor (`pkg/privacy/redact.go`)** [🟢 Stable]
  - Eliminate aggressive body regexes that corrupt Shopify `gid://`, git diffs, and XML tags (`<merchant_data>`).
  - Restrict redaction exclusively to Authorization headers and verified API key prefixes (`sk-ant-`, `sk-proj-`, `AIzaSy`).
- [ ] **1:1 Bitwise Transparent Streaming Passthrough** [🟢 Stable]
  - Direct wire streaming for matching dialects without intermediate JSON deserialization or SSE boundary alteration.
- [ ] **Automated Non-Destructive Migrations (`pkg/identity/migrate.go`)** [🟢 Stable]
  - Safe migration path converting legacy profiles into normalized `identities.json` schemas with zero data loss.

**Success Criteria**:
- 100% test pass rate across all packages (`go test ./...`).
- Zero plaintext secrets stored on disk across any supported platform.
- CLI execution time < 15ms for all local status and identity queries.

---

## 3. 60-Day Milestone: Universal Tool Engine & Adaptive Gateway

**Primary Objective**: Deliver zero-lag, protocol-agnostic tool execution and smart quota-based failover between subscription accounts, web sessions, and API endpoints.

### Deliverables & Task Breakdown:
- [ ] **Universal Tool Engine (`pkg/tools/`)** [🟡 Experimental ➔ 🟢 Stable]
  - Formalize Canonical Intermediate Representation: `UniversalTool`, `UniversalToolCall`, `UniversalToolResult`, and `UniversalToolError`.
  - Bidirectional adapters with 100% JSON Schema constraint preservation for:
    - Anthropic Claude (`tool_use` / `tool_result`)
    - OpenAI / Codex (`tool_calls` / `role: "tool"`)
    - Google Gemini (`functionCall` / `functionResponse`)
    - Agent Text-Loop (`<tool_call>` parsing & XML encapsulation)
- [ ] **Dynamic Conditional IDE Hooking & Auto-Detach Engine** [🟡 Experimental]
  - Silent OS Keychain rotation across healthy subscription accounts.
  - Automatic injection of gateway proxy (`127.0.0.1:8787`) *only* when all subscriptions hit quota threshold (95% for multi-account pools, 100% for single-account pools).
  - Immediate auto-detachment and restoration of direct upstream settings upon quota reset.
- [ ] **High-Fidelity Web-Loop Emulation Harness** [🟡 Experimental]
  - Robust multi-turn session handling for browser-authenticated developer workflows.
  - Synthetic tool schema injection and SSE stream emulation.
- [ ] **Real-Time Telemetry & Observability (`pkg/telemetry/`)** [🟢 Stable]
  - Millisecond-precision token accounting and rolling request latency tracking.
  - Local SQLite / structured log sink with zero cloud exfiltration.

**Success Criteria**:
- Zero syntax or schema loss in round-trip tool conversions across Anthropic, OpenAI, and Gemini.
- Zero-downtime failover during mid-session token limit exhaustion.
- Less than 1ms overhead added by the local gateway proxy.

---

## 4. 90-Day Milestone: AI-Native Agent Ecosystem & Platform Layers

**Primary Objective**: Transform AMUX into an operating system for human-AI engineering collaboration, supporting multi-agent delegation, shared project context, and local LLM execution.

### Deliverables & Task Breakdown:
- [ ] **Multi-Agent Coordination & Delegation Layer (`pkg/agent/`)** [🔵 Planned]
  - Structured orchestration interfaces for specialized agents (Product, Architect, Engineer, Reviewer, Research).
  - Subagent task tracking, lifecycle control, and asynchronous message bus.
- [ ] **Project Context Graph & Synchronizer (`pkg/context/`)** [🔵 Planned]
  - Cross-IDE shared state synchronizer to preserve developer intention across Claude Code, Cursor, and Codex CLI.
  - Intelligent context compaction triggered seamlessly on mid-session model or account rotation.
- [ ] **Universal Model Context Protocol (MCP) Broker** [🔵 Planned]
  - Native MCP server discovery, connection pooling, and lifecycle management.
  - Bidirectional translation between MCP tool servers and native IDE dialects.
- [ ] **Local LLM Runtime Bridge** [🔵 Planned]
  - Unified routing to local inference engines (Ollama, vLLM, llama.cpp, LocalAI) alongside cloud providers.
  - Automated offline fallback when internet connectivity is interrupted.
- [ ] **Enterprise Team Vault & RBAC Federation** [🔵 Planned]
  - Opt-in team key sharing backed by encrypted envelope distribution and role-based access policies.

**Success Criteria**:
- Simultaneous multi-agent collaboration with end-to-end task traceability.
- Universal MCP support allowing any standard MCP tool to be used across all connected IDEs.
- Frictionless offline local inference fallback.

---

## 5. Release & Quality Governance

Every release of AMUX follows strict quality gates:

1. **Test Coverage**: All new packages must include unit and integration tests; overall repo test pass rate must remain at **100%**.
2. **Backwards Compatibility**: Migration paths must be non-destructive. User configuration files must be upgraded automatically without requiring manual edits.
3. **Security Auditing**: Every release must pass `amux audit` and `amux doctor --security` validation checks.
4. **Performance Budgets**:
   - Memory footprint (idle daemon): < 25 MB RAM.
   - Gateway proxy latency overhead: < 2 ms.
   - CLI dispatch latency: < 15 ms.
