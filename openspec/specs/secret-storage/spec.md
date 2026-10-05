# secret-storage Specification

## Purpose
Keep every secret amux handles either in the OS keychain or in an encrypted file vault, so amux can run without ever touching the keychain.

## Requirements
### Requirement: Selectable secret backend
Every secret read or write in amux SHALL go through one backend chosen by `$AMUX_SECRET_STORE` (`file`|`keychain`), `$AMUX_NO_KEYCHAIN=1`, the persisted `~/.amux/secret_store`, then the platform default (keychain on macOS, file elsewhere, file inside test binaries).

#### Scenario: Environment override
- **WHEN** `AMUX_SECRET_STORE=file` is set
- **THEN** no `security` command or keyring call is made

### Requirement: Encrypted file vault
In file mode secrets SHALL be stored in `~/.amux/secrets.vault` as `AMENC1:` AES-256-GCM ciphertext (mode `0600`) keyed by `$AMUX_MASTER_KEY` or `~/.amux/master.key`; the master key MUST NOT be read from or written to the OS keyring.

#### Scenario: Vault at rest
- **WHEN** a secret is stored in file mode
- **THEN** the vault file contains no plaintext secret

### Requirement: Claude credential compatibility
In file mode, Claude Code credentials SHALL be mirrored to Claude Code's own credential file (`$CLAUDE_CONFIG_DIR/.credentials.json` or `~/.claude/.credentials.json`) and read from it when the vault lacks them.

#### Scenario: Token written by Claude Code
- **WHEN** Claude Code logged in without a keychain and the vault is empty
- **THEN** the gateway reads the live token from Claude Code's credential file

### Requirement: Safe switching
`amux config secret-store file` SHALL first export the keyring master key to `~/.amux/master.key` (so `AMENC1:` files stay readable) and copy the keychain items amux uses into the vault; afterwards amux MUST NOT access the keychain.

#### Scenario: Encrypted accounts after switching
- **WHEN** `accounts.json` was sealed with the keyring key and the user switches to file mode
- **THEN** the gateway still decrypts it

