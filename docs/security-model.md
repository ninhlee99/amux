# AMUX Security & Trust Model

This document outlines the security architecture, threat model, credential lifecycle, and trust guarantees of AMUX.

---

## 1. Core Trust Principles

AMUX is designed for professional software engineers who work across multiple AI developer tools (Claude Code, Cursor, OpenAI Codex, Google Antigravity/Gemini). Our security architecture is guided by four foundational principles:

1. **Zero Unencrypted Credentials at Rest**: API keys, OAuth refresh tokens, and session cookies are never written to disk in plain text.
2. **Hardware & OS-Bound Secret Storage**: Encryption keys are backed by the host's native OS Keyring (macOS Keychain, Linux Secret Service / SecretStorage) whenever available.
3. **Localhost-Only Network Boundaries**: The local gateway daemon listens exclusively on `127.0.0.1:8787` with token authentication. It is strictly forbidden from binding to `0.0.0.0` or exposing ports over external networks.
4. **Crash Safety & Native Execution**: AMUX synchronizes credentials directly into native IDE secret stores so that if the AMUX daemon is stopped or crashes, your IDEs continue functioning normally without downtime.

---

## 2. Identity & Secret Lifecycle

```text
[ Developer Login / Ingestion ]
             │
             ▼
[ Host-Specific Master Key ] (Derived via OS Keyring / 256-bit AES-GCM)
             │
             ▼
[ Encrypted Vault on Disk ] (~/.amux/identities.json with AMENC1: envelope, mode 0600)
             │
             ├──────────────────────────────────────────┐
             ▼                                          ▼
   [ Native OS Keychain Sync ]             [ Local Gateway Proxy ]
 (Claude Code / AGY Credentials)         (Localhost:8787, In-Memory Only)
             │                                          │
             ▼                                          ▼
   [ Direct CLI Execution ]                 [ Stream & Quota Routing ]
 (Zero-Touch Native IDE Runtime)             (Tokens Redacted in Logs)
```

### Encryption at Rest (`AMENC1:` Specification)
All persistent identity documents (`~/.amux/identities.json`, `~/.amux/accounts.json`, and `.amp` profile bundles) are sealed with AES-256-GCM authenticated encryption.
- **Envelope Format**: Base64 encoded payload prefixed with the magic header `AMENC1:`.
- **Key Derivation**: A 256-bit cryptographically secure random master key generated on first initialization and stored in the OS Keyring (`am-master-key` service).
- **File System Permissions**: Root directory `~/.amux` is locked to mode `0700` (read/write/exec by owner only); all secret files are locked to mode `0600` (read/write by owner only).

---

## 3. Threat Model & Mitigations

| Threat Vector | Risk Description | AMUX Mitigation Strategy |
|---|---|---|
| **Local Disk Exfiltration / Malware** | Malicious software attempts to read configuration files in `~/.amux`. | All credential files are AES-256-GCM encrypted. Without access to the user's OS Keychain master key, raw files cannot be decrypted. |
| **Local Port Sniffing** | Another local process attempts to snoop or hijack gateway traffic. | Gateway binds strictly to `127.0.0.1`. Each gateway session requires local loopback authorization headers. |
| **Accidental Token Leakage in Logs** | Diagnostics, error logs, or terminal outputs leak API keys or bearer tokens. | Integrated privacy redact engine (`pkg/privacy/redact.go`) automatically scans and masks tokens (e.g. `sk-ant-***`, `Bearer ***`) before writing to log sinks. |
| **Account Collision & Overwriting** | Multiple logins accidentally overwrite or downgrade a paid subscription. | Strict 1-account-per-email rule per provider with hierarchical precedence (Subscription tier strictly supersedes Web session tier). |
| **Malicious Remote Command Execution** | Remote API payloads attempt unauthorized local tool execution. | Capability runtime enforces strict schema validation and whitelist constraints for all MCP and native tool calls. |

---

## 4. Credential Revocation & Auditing

### Verifying Security Posture
Developers can audit their local security configuration at any time:
```bash
# Run security and environment diagnostics
$ amux doctor

# Inspect active identities, encryption state, and thresholds
$ amux id list
```

### Immediate Revocation
To permanently revoke and purge credentials:
```bash
# Remove a specific identity from both vault and native keychain
$ amux id remove <identity-id>

# Hard-disable an identity from routing without deleting configuration
$ amux id disable <identity-id>
```

---

## 5. Non-Interference & Failure Modes

### What happens if AMUX crashes or is uninstalled?
- **Claude Code & Codex**: Continue working uninterrupted. Their active credentials are saved directly in native storage (`Claude Code-credentials` in Keychain and `~/.claude.json`).
- **Google Antigravity (AGY)**: Continues working via native `credentials.json`.
- **Web Session Proxies**: If the background daemon is not running, requests fallback to direct API keys or report that the local gateway is stopped. Run `amux start` to resume the gateway.
