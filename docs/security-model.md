# AMUX Security & Trust Model

This document specifies the AMUX security architecture, trust model, credential encryption mechanisms, and permission lifecycle.

---

## 1. Core Trust Philosophy & Boundaries

AMUX is designed for professional software engineers who manage multiple AI development environments, subscriptions, and tools (Claude Code, Cursor, OpenAI Codex, Google Antigravity/Gemini). Our architecture is built on four non-negotiable trust principles:

1. **Zero Unencrypted Credentials at Rest**: API keys, OAuth session tokens, refresh tokens, and cookies are never stored in plain text on disk.
2. **OS-Native Hardware & Keyring Binding**: Master encryption keys are anchored directly to host OS keychains (macOS Keychain, Linux Secret Service) rather than static configuration files.
3. **Localhost-Only Network Boundaries**: The AMUX gateway daemon binds strictly to `127.0.0.1:8787` (loopback). It does not expose open ports to the local network or public internet by default.
4. **Zero Telemetry Exfiltration**: AMUX performs no remote telemetry tracking, prompt logging, or token extraction to external servers. All operations execute strictly on your workstation.
5. **Non-Destructive Failure Modes**: In zero-touch mode, AMUX synchronizes credentials directly into native IDE secret stores. If AMUX is stopped, uninstalled, or crashes, developer tools (Claude Code, Codex, Cursor) continue operating natively without disruption.

---

## 2. Architecture & Data Flow

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Developer Workstation                          │
│                                                                        │
│  [ Developer CLI / Web Login ]                                         │
│                │                                                       │
│                ▼                                                       │
│  [ OS Keychain / Secret Service ] ──▶ Hardware-backed Master Key       │
│                │                                │                      │
│                ▼                                ▼                      │
│  [ Local Encrypted Vault ] ─────────▶ AES-256-GCM (AMENC1: Envelope)   │
│   (~/.amux/identities.json)            File Permissions: 0600          │
│                │                                                       │
│        ┌───────┴────────────────────────┐                              │
│        ▼                                ▼                              │
│  [ Native Keychain Sync ]       [ Localhost Gateway Daemon ]           │
│   • Claude Code Credentials       • 127.0.0.1:8787                     │
│   • Antigravity Service           • In-Memory Proxy Stream             │
│   • IDE Config Sync               • Privacy Redaction Engine           │
│        │                                │                              │
│        ▼                                ▼                              │
│  [ Direct CLI Execution ]       [ Upstream Provider APIs ]             │
│   (Zero-touch native run)        (Anthropic, OpenAI, Google)           │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Cryptographic Storage Specification

### 3.1. Master Key Derivation & OS Keychain Integration
AMUX uses a tiered strategy to acquire and protect the master encryption key:

1. **Primary (OS Native Keyring)**:
   - **macOS**: Apple Keychain Services via native Security framework (`amux-encryption-key` in `login.keychain-db`).
   - **Linux**: Freedesktop Secret Service API via D-Bus / SecretStorage (GNOME Keyring, KWallet).
   - On first run, AMUX generates a cryptographically secure 256-bit random key (`crypto/rand`) and commits it directly to the OS keyring.

2. **Headless & Container Fallback**:
   - In environments without a graphical D-Bus session or macOS Keychain daemon (e.g., CI/CD, remote SSH sessions, Docker containers), AMUX derives a deterministic host-specific key using **PBKDF2 (HMAC-SHA256)** with 100,000 iterations, combining machine machine-id (`/etc/machine-id` or `sysctl hw.uuid`), user UID, and local salt.

### 3.2. `AMENC1:` Authenticated Encryption Envelope
Every persistent secret file (`identities.json`, cached session pools, and identity profiles) is sealed using authenticated symmetric encryption:

```text
AMENC1:<Base64( 12-byte Nonce || AES-256-GCM Ciphertext || 16-byte Poly1305 Tag )>
```

- **Cipher**: AES-256 in Galois/Counter Mode (GCM), providing both confidentiality and cryptographic integrity verification.
- **Nonce**: Unique 96-bit (12-byte) initialization vector generated per write from `crypto/rand`.
- **Integrity Guarantee**: Any bit-level tampering or corruption of the encrypted file is detected immediately during decryption, causing an atomic abort rather than loading corrupted credentials.

### 3.3. POSIX Permission Hardening
AMUX enforces strict file system access controls at the operating system level:
- Storage root directory (`~/.amux/`): Enforced mode `0700` (`drwx------`, readable/writable only by the owner).
- All secret and database files: Enforced mode `0600` (`-rw-------`, readable/writable only by the owner).
- AMUX verifies and repairs file permissions automatically on every CLI invocation.

---

## 4. Permission & Credential Lifecycle

The lifecycle of credentials inside AMUX progresses through four explicit phases:

```text
  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │  1. INGEST   │ ──▶ │  2. SWITCH   │ ──▶ │  3. RUNTIME  │ ──▶ │  4. REVOKE   │
  │ (Auth/Login) │     │ (Activation) │     │ (Proxy/Exec) │     │ (Purge/Off)  │
  └──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
```

### Phase 1: Ingestion & Verification
- When a user logs in via `amux login <provider>` (OAuth PKCE, browser session, or API key):
  1. Temporary tokens are acquired in memory.
  2. Tokens are validated via a lightweight health probe to the upstream provider.
  3. The payload is encrypted with the OS master key and written atomically to `~/.amux/identities.json.tmp` before renaming to `~/.amux/identities.json` (preventing partial write corruption).

### Phase 2: Activation & Keychain Synchronization (`amux switch`)
- When an identity is selected via `amux switch <id>`:
  1. AMUX loads and decrypts the target identity from the local vault.
  2. The active credentials are synchronously written to the system's native storage:
     - **Claude Code**: Written to macOS Keychain / OS Keyring under service `Claude Code-credentials`.
     - **Google Antigravity**: Written to the application's authenticated session directory.
     - **OpenAI Codex**: Updated in `~/.codex/config.json`.
  3. Native tools can now be invoked immediately in the terminal without browser popups or re-authentication prompts.

### Phase 3: Runtime Isolation & Privacy Redaction
- When requests flow through the local gateway (`127.0.0.1:8787`):
  1. Credential decryption occurs purely in-memory. Secrets are never cached in unencrypted temporary files.
  2. **Privacy Redaction Engine (`pkg/privacy/redact.go`)**: Scans all egress logs, stdout streams, and error events to mask sensitive patterns (`sk-ant-***`, `Bearer ***`, cookie strings).
  3. **Runtime Execution Sandbox (`pkg/runtime`)**: MCP and CLI tool invocations enforce strict argument schema validation and execution timeouts.

### Phase 4: Revocation & Deprovisioning
- When removing an identity via `amux account logout <id>` or `amux uninstall`:
  1. The identity is expunged from `identities.json` and disk space is securely overwritten.
  2. Corresponding OS Keychain entries (`Claude Code-credentials`, etc.) are purged via native keyring APIs.
  3. IDE configurations patched by `amux hook` are restored to their original unhooked states via `amux unhook --all`.

---

## 5. Threat Model Matrix

| Threat Vector | Attack Scenario | AMUX Defense & Mitigation |
|:---|:---|:---|
| **Local Disk Exfiltration** | An untrusted application or script reads `~/.amux/identities.json`. | Files are AES-256-GCM encrypted. Without access to the OS Keychain master key (protected by OS user session), ciphertext cannot be decrypted. |
| **Local Port Snooping** | A malicious local process attempts to connect to `localhost:8787` to hijack sessions. | Gateway binds strictly to loopback (`127.0.0.1`). All requests require loopback authentication bearer tokens. |
| **Log Leakage** | API keys or session cookies accidentally appear in terminal outputs or debug logs. | The `pkg/privacy` redact layer intercepts all log sinks and strips tokens, auth headers, and session cookies before display or persistence. |
| **Cross-Account Collision** | Multiple logins for the same provider overwrite existing tokens or downgrade subscription tiers. | Strict 1-account-per-provider deduplication model with hierarchical tier precedence (Subscription > Web Session). |
| **Process Crash / Panic** | AMUX daemon encounters an unexpected termination during an active coding session. | Zero-touch keychain sync ensures IDEs already possess valid credentials in their native stores; tools continue executing directly against upstream APIs. |

---

## 6. Security Diagnostics & Auditing

AMUX includes built-in commands for developers to verify their security posture:

```bash
# Verify keychain access, file permissions, and cryptographic health
amux doctor --security

# Audit vault integrity, AES-256-GCM status, and active token states
amux audit

# Inspect active identities and auto-rotation thresholds
amux account list
```
