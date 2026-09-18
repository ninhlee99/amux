# AMUX Architecture Audit & Refactoring Roadmap

**Mission:** Transform AMUX into a production-grade, non-invasive AI-native development infrastructure layer:
```
Human Developer
      ↓
AI Applications (Claude Code, Codex CLI, Cursor, Gemini/AGY, Autonomous Agents)
      ↓
AMUX Runtime Layer (Identity Layer, Context Layer, Universal AI Gateway, Universal Tool Engine)
      ↓
Providers & Models (Anthropic, OpenAI, Gemini, Local, API Endpoints)
```

---

## 1. Executive Summary

AMUX was initially conceived as an account switcher and reverse proxy for local CLI tools. Over time, features were added organically, resulting in:
1. **Monolithic & Entangled CLI Surface**: `pkg/cli/cli.go` (1,947 lines) contains mixed concerns—CLI argument parsing, shell environment manipulation, process execution (`amux run <app>`), background daemon management, and accounts display.
2. **Invasive Workflow & Alias Bloat**: Reliance on the legacy alias `am` and launcher commands (`amux run claude`, `amux run codex`), violating the core architectural principle that **AMUX must be non-invasive and zero-touch** by default. Developers should run their native tools (`claude`, `codex`, `cursor`) normally.
3. **Fragmented Identity Model**: Account storage is divided between `pkg/types/account.go`, `pkg/profile/manager.go`, `accounts.json`, and OS Keychain entries, with remnants of legacy group concepts. Missing a unified, flat `Identity` schema with automated silent Keychain rotation across subscription tiers.
4. **Conditional Gateway & Threshold Mechanics**: Lack of a clear lifecycle where the gateway is detached and zero-touch until **all** subscription accounts of a provider reach capacity threshold (95% for multi-account pools, 100% for single-account pools), after which the gateway hook is conditionally injected and later auto-detached upon quota reset.
5. **Tool Mid-Layer Dialect Gaps**: `pkg/tools` contains separate converter functions, but lacks the formal **Canonical Tool Model** (`UniversalTool`, `UniversalToolCall`, `UniversalToolResult`, `UniversalToolError`) with guaranteed schema preservation across Anthropic, OpenAI, Gemini, MCP, and Web-Loop tool emulation.
6. **Aggressive Privacy Over-Redaction**: `pkg/privacy/redact.go` (1,044 lines) applies generic regexes across whole message bodies, causing payload corruption on Shopify global IDs (`gid://`), git commit SHAs, unified diffs, XML tags (`<merchant_data>`), and function arguments.
7. **Artificial Latency & Bloat**: `pkg/guard/pacer.go` introduces artificial delays and micro-jitter into requests, degrading local developer experience.

This audit establishes the baseline for the complete 7-phase refactoring plan.

---

## 2. Current vs. Target Architecture Comparison

| Dimension | Current Architecture | Target Architecture (`refactor/amux-modern-runtime`) |
| :--- | :--- | :--- |
| **Binary & Alias** | Binary `amux` with alias `am` everywhere; multiple entry points | Single binary `amux`; `am` alias completely removed; entry point in `cmd/amux/main.go` |
| **CLI Implementation** | Monolithic `pkg/cli/cli.go` (~1,950 lines) | Minimal `cli.go` (< 80 lines) delegating to modular handlers (`id.go`, `gateway.go`, `setup.go`, `doctor.go`, `config.go`, `migrate.go`) |
| **Application Launching** | `amux run <app>` launches child IDE processes | **Zero application launching**. Users execute `claude`, `cursor`, `codex` directly |
| **IDE Integration** | Permanent proxy environment exports / hook injection | **Zero-touch by default**. Native OS Keychain used directly. Silent Keychain rotation across subscriptions; Gateway conditionally hooked ONLY when all subscriptions hit threshold (95% multi / 100% single), and auto-detached on quota reset |
| **Identity Model** | Dispersed `types.Account`, `profile.Manager`, `accounts.json`, groups | Flat `Identity` schema in `pkg/identity/` (ID, Provider, Tier, AuthType, Credentials, UsagePercent, ResetAt, Active, Metadata) |
| **Gateway Lifecycle** | `pkg/proxy/` coupled to Claude rotator and manual commands | `pkg/gateway/` with detached background daemon, socket status, graceful shutdown, and clean dynamic IDE hooking |
| **Passthrough Fidelity** | Parses & reformats requests even for matching upstreams | **1:1 bitwise streaming passthrough** when `ClientDialect == TargetDialect` (no intermediate JSON parse, exact SSE preserved) |
| **Tool Engine** | Ad-hoc dialect translation in `pkg/tools/` | **Universal Tool Engine** with Canonical IR (`UniversalTool`, `UniversalToolCall`, `UniversalToolResult`, `UniversalToolError`), bidirectional adapters (Anthropic, OpenAI, Gemini, MCP, Web-Loop), and 100% schema constraint preservation |
| **Redaction Scope** | Generic regexes scanning body for emails, IPs, lat/lng, crypto | **Strict Target Isolation**: Authorization headers and standalone API keys (`sk-ant-`, `sk-proj-`, `sk-`, `AIzaSy`, `ghp_`). Zero touching of `gid://`, hashes, diffs, XML, tools |
| **Traffic Pacing** | `pkg/guard/pacer.go` micro-jitter & delays | **Pacer eliminated**. Zero artificial latency added to developer requests |
| **Observability** | Scattered logging across proxy, monitor, and term | Structured millisecond-precision real-time logging in `pkg/telemetry/` |

---

## 3. Target Directory & Package Structure

```
amux/
├── cmd/
│   └── amux/
│       └── main.go                 # Minimal binary entry point (< 25 lines)
├── pkg/
│   ├── cli/                        # Decomposed minimal CLI surface
│   │   ├── cli.go                  # Dispatcher (< 80 lines)
│   │   ├── setup.go                # Guided wizard
│   │   ├── status.go               # Real-time dashboard & quotas
│   │   ├── doctor.go               # System & tool diagnostics
│   │   ├── id.go                   # Flat identity management (add, list, remove, health, select)
│   │   ├── gateway.go              # Gateway management (start, stop, status, hook, unhook)
│   │   ├── config.go               # Configuration inspection
│   │   └── migrate.go              # Non-destructive data migration
│   ├── identity/                   # Credentials, Keychain rotation, flat identity model
│   │   ├── identity.go             # Canonical Identity & Config definitions
│   │   ├── store.go                # Persistence & safe atomic read/write
│   │   ├── keychain.go             # macOS Keychain rotation & native credential sync
│   │   ├── threshold.go            # 95% multi-account / 100% single-account failover rules
│   │   ├── health.go               # Token validity and quota checking
│   │   └── migrate.go              # Auto-migration from legacy profiles & accounts.json
│   ├── gateway/                    # Modern Universal AI Gateway
│   │   ├── server.go               # High-performance HTTP server & reverse proxy
│   │   ├── passthrough.go          # 1:1 bitwise transparent streaming passthrough
│   │   ├── hook.go                 # Conditional IDE injection & auto-detachment engine
│   │   ├── daemon.go               # Detached process supervisor & lifecycle
│   │   └── middleware.go           # Sanitization & rate-limit handling
│   ├── router/                     # Quota-aware routing & session affinity
│   │   ├── pool.go                 # Tier-ordered routing (Subscription -> Web -> API Key)
│   │   ├── affinity.go             # Session pinning for multi-turn conversations
│   │   └── classifier.go           # Fast request classification (no tool bias)
│   ├── tools/                      # Universal Tool Engine
│   │   ├── canonical.go            # UniversalTool, Call, Result, Error IR definitions
│   │   ├── adapter.go              # ToolAdapter interface specification
│   │   ├── anthropic.go            # Anthropic Claude adapter
│   │   ├── openai.go               # OpenAI Function Calling adapter
│   │   ├── gemini.go               # Gemini Function Calling adapter
│   │   ├── mcp.go                  # Model Context Protocol adapter
│   │   ├── textloop.go             # Agent text-loop parser (<tool_call> blocks)
│   │   ├── webloop.go              # Web Session emulation harness
│   │   └── protect.go              # Syntax, XML, domain ID & diff protection
│   ├── context/                    # Project context & memory
│   │   ├── context.go              # Context state propagation
│   │   └── compact.go              # Safe compaction (active only on mid-session rotation)
│   ├── agent/                      # Multi-agent coordination & delegation
│   │   └── agent.go                # Coordination interfaces & subagent tracking
│   ├── telemetry/                  # Logging, metrics & distributed tracing
│   │   ├── logger.go               # Real-time structured millisecond logging
│   │   └── metrics.go              # Token counters & latency metrics
│   ├── privacy/                    # Strictly isolated secret redactor
│   │   └── redact.go               # Targeted header & API key scanner (no payload mangling)
│   ├── usage/                      # Token accounting & rolling quota calculators
│   │   ├── capture.go              # Streaming token extractor
│   │   └── window.go               # 5-hour / 7-day rolling quota tracking
│   └── ui/                         # Terminal presentation & interactive pickers
│       ├── picker.go               # Fuzzy account selector
│       └── status.go               # Status dashboard renderer
```

---

## 4. Component-by-Component Analysis & Migration Strategy

### 4.1 CLI Modernization & Bloat Elimination (`pkg/cli/`)
- **Current Problem**: `pkg/cli/cli.go` is 1,947 lines, handles `amux run <app>`, has multiple alias references (`am`), and bundles diverse commands in one massive switch statement.
- **Migration Strategy**:
  1. Extract commands into single-purpose files: `setup.go`, `status.go`, `doctor.go`, `id.go`, `gateway.go`, `config.go`, `migrate.go`.
  2. Reduce `pkg/cli/cli.go` to < 80 lines: dispatch logic only.
  3. Delete `cmdRun` and all launcher responsibilities (`amux run claude`, `amux run codex`, etc.).
  4. Deprecate and remove the `am` binary alias.

### 4.2 Flat Identity Model & Dynamic Keychain Failover (`pkg/identity/`)
- **Current Problem**: Fragmented storage across `~/.am/accounts.json`, `~/.am/profiles/`, and Keychain. Group abstractions (`pkg/router/group.go`) add unnecessary cognitive load and routing anomalies.
- **Target Design**:
  ```go
  type IdentityTier string
  const (
      TierSubscription IdentityTier = "subscription"
      TierWeb          IdentityTier = "web"
      TierAPIKey       IdentityTier = "api_key"
  )

  type Identity struct {
      ID           string                 `json:"id"`
      Provider     string                 `json:"provider"` // anthropic, openai, gemini, cursor
      Tier         IdentityTier           `json:"tier"`     // subscription, web, api_key
      AuthType     string                 `json:"auth_type"` // oauth, cdp, api_key, session_cookie
      Credentials  map[string]string      `json:"credentials"`
      UsagePercent float64                `json:"usage_percent"`
      ResetAt      int64                  `json:"reset_at,omitempty"`
      Active       bool                   `json:"active"`
      Metadata     map[string]interface{} `json:"metadata,omitempty"`
  }
  ```
- **Keychain Rotation & Threshold Mechanics**:
  - Pool with $N > 1$ subscriptions: Rotate to next subscription at 95.0% threshold.
  - Pool with $N = 1$ subscription: Allow reaching 100.0% before fallback.
  - Silent OS Keychain update writes active token directly into `Claude Code-credentials` (or equivalent) so IDE executes native upstream requests without proxying.
  - Conditional Gateway Injection: When **all** subscriptions hit their threshold, AMUX injects the gateway URL (`http://127.0.0.1:8787`) into the IDE settings (`~/.claude/settings.json`, etc.).
  - Auto-Detachment: When a subscription resets (`UsagePercent < ThresholdPct`), gateway hook is automatically removed from IDE settings.
  - Non-destructive migration: `amux migrate` converts legacy profiles into `identities.json` safely.

### 4.3 Universal AI Gateway (`pkg/gateway/`)
- **Current Problem**: Proxy is located in `pkg/proxy/`, mixed with legacy naming, and routes traffic through parsing bridges even when target matches client dialect.
- **Target Design**:
  - `pkg/gateway/`: Standalone detached daemon.
  - **1:1 Bitwise Streaming Passthrough**: If client dialect matches target provider dialect (e.g., Claude Code -> Anthropic API), stream raw bytes directly. No JSON unmarshal, zero mutation of SSE boundaries, headers, or parameters.
  - When target provider differs (e.g., Claude Code -> OpenAI backend), route through `pkg/tools/` Universal Tool Engine.

### 4.4 Universal Tool Engine (`pkg/tools/`)
- **Canonical Architecture**:
  ```
  Inbound Payload -> Tool Adapter -> Canonical UniversalTool IR -> Security/Redactor -> Target Adapter -> Outbound Wire
  ```
- **Canonical Structures**:
  - `UniversalTool`: ID, Name, Description, InputSchema (`map[string]interface{}`), Source, Metadata.
  - `UniversalToolCall`: ID, Name, Arguments (`map[string]interface{}`), RawJSON.
  - `UniversalToolResult`: ToolCallID, Success, Output string, Error (*UniversalToolError), Metadata.
  - `UniversalToolError`: Code, Message, Retryable, Provider.
- **Bidirectional Adapters**:
  - Anthropic: `tool_use` / `tool_result`
  - OpenAI: `tool_calls` / `role: "tool"`
  - Gemini: `functionCall` / `functionResponse`
  - MCP: `tools/list` / `tools/call`
  - Agent Text-Loop: `<tool_call>{"name":"...","arguments":{...}}</tool_call>`
  - Web-Loop: Injection of system tool schema and SSE stream parsing.
- **Strict Schema Preservation**: Zero dropping of JSON Schema attributes (`properties`, `required`, `items`, `oneOf`, `anyOf`, `default`, `description`, `additionalProperties`).

### 4.5 Payload Protection & Isolated Redactor (`pkg/privacy/`)
- **Current Problem**: `pkg/privacy/redact.go` scans body text with aggressive regexes for emails, phones, locations, crypto, etc.
- **Refactored Rule**:
  - ONLY scan Authorization headers (`Bearer`, `x-api-key`, `Cookie: session=...`) and standalone API keys matching known prefixes (`sk-ant-`, `sk-proj-`, `sk-`, `AIzaSy`, `ghp_`).
  - Strict exclusion for `gid://shopify/...`, git commit SHAs, unified diff headers, XML tags (`<merchant_data>`, `<tools>`, `<thinking>`), and tool parameters.

### 4.6 Bloat Removal & Latency Elimination
- Delete `pkg/guard/pacer.go`: Remove all artificial delays and jitter.
- Delete unused group hierarchy and task preference biases.
- Keep session affinity, health monitoring, and quarantine.

---

## 5. Test Gap Analysis & Test Matrix Plan

Existing tests (`pkg/bridge/cross_tool_matrix_test.go`, `pkg/tools/webloop_test.go`) provide a good foundation for wire protocols, but key gaps exist:

| Test Gap | Vulnerability / Risk | Mitigation Plan (Phase 6) |
| :--- | :--- | :--- |
| **Fidelity of Protected Patterns** | XML tags (`<merchant_data>`), Shopify `gid://`, git diffs could be mangled by redactor or tool adapter | Add `TestPayloadFidelity_ProtectedPatterns` asserting bit-for-bit survival |
| **1:1 Bitwise Passthrough** | Passthrough might alter SSE chunk boundaries or strip unknown Anthropic headers | Add `TestGateway_BitwisePassthrough` with raw chunk comparison |
| **Silent Keychain Rotation** | Race condition or failure when swapping OS Keychain credentials at 95% threshold | Add `TestKeychainRotation_MultiAccountThreshold` & `TestKeychainRotation_SingleAccountLimit` |
| **Conditional Hook & Auto-Detach** | Settings file could be corrupted or remain hooked after quota resets | Add `TestGateway_ConditionalHookingAndAutoDetach` |
| **Bidirectional MCP Adapter** | MCP manifest or call could lose parameter types during conversion | Add `TestUniversalTool_MCP_Bidirectional` |
| **Web-Loop Multi-Turn Continuity** | Multi-turn tool results might fail to append cleanly as user turns | Add `TestWebLoop_MultiTurnRoundTrip` |

---

## 6. Execution Milestones & Git Commit Specification

1. **Phase 1: Audit & Architecture Report**
   - File: `ARCHITECTURE_REPORT.md`
   - Commit: `docs: complete architecture audit and refactoring roadmap`
2. **Phase 2: CLI Modernization & Decomposition**
   - Decompose `pkg/cli/cli.go` into `setup.go`, `status.go`, `doctor.go`, `id.go`, `gateway.go`, `config.go`, `migrate.go`.
   - Remove `am` alias and launcher commands.
   - Commit: `refactor(cli): simplify command structure and remove launcher responsibility`
3. **Phase 3: Identity Layer & Flat Migration**
   - Implement `pkg/identity/`, silent Keychain rotation, and threshold engine.
   - Non-destructive migration from legacy accounts.
   - Commit: `refactor(identity): introduce flat identity model and keychain rotation`
4. **Phase 4: Universal AI Gateway & Non-Invasive Hooks**
   - Implement `pkg/gateway/` with 1:1 bitwise streaming passthrough and conditional dynamic hooking.
   - Commit: `refactor(gateway): transition from proxy to universal gateway with dynamic hooking`
5. **Phase 5: Universal Tool Engine & Syntax Integrity**
   - Implement canonical `UniversalTool` IR and adapters (Anthropic, OpenAI, Gemini, MCP, Web-Loop).
   - Constrain `pkg/privacy/redact.go` to strictly protect payload syntax and domain IDs.
   - Commit: `refactor(tools): implement universal tool engine and payload fidelity layer`
6. **Phase 6: Observability, Testing & Validation**
   - Telemetry structured logging; complete cross-dialect test matrix.
   - Ensure `go build ./...` and `go test -v ./...` succeed with 0 errors.
   - Commit: `test(matrix): add cross-dialect tool and payload integrity test suites`
7. **Phase 7: Documentation Rewrite**
   - Complete rewrite of `README.md`.
   - Commit: `docs: rewrite documentation for modern amux architecture`
