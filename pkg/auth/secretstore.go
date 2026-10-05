package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"amux-accounts/pkg/types"

	"github.com/zalando/go-keyring"
)

// Secret store backends. Every KCGet/KCSet/KCAccount call in amux goes
// through the selected backend, so switching needs no caller changes.
const (
	// SecretStoreKeychain uses the macOS Keychain (`security`): required for
	// native, gateway-less rotation because Claude Code reads its token there.
	SecretStoreKeychain = "keychain"
	// SecretStoreFile keeps secrets in ~/.amux/secrets.vault, AES-256-GCM
	// encrypted with ~/.amux/master.key (or $AMUX_MASTER_KEY). No OS keychain
	// prompt ever appears; IDEs authenticate to the amux gateway instead.
	SecretStoreFile = "file"
)

var errSecretNotFound = errors.New("secret not found")

func secretStoreSettingPath() string { return filepath.Join(types.BaseDir(), "secret_store") }
func secretVaultPath() string        { return filepath.Join(types.BaseDir(), "secrets.vault") }
func masterKeyFilePath() string      { return filepath.Join(types.BaseDir(), "master.key") }

// SecretStore reports the active backend. Precedence: $AMUX_SECRET_STORE,
// $AMUX_NO_KEYCHAIN=1, ~/.amux/secret_store, then the platform default
// (keychain on macOS, file elsewhere — and file inside test binaries so tests
// can never touch the developer's real keychain).
func SecretStore() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AMUX_SECRET_STORE"))) {
	case SecretStoreFile:
		return SecretStoreFile
	case SecretStoreKeychain:
		return SecretStoreKeychain
	}
	if os.Getenv("AMUX_NO_KEYCHAIN") == "1" {
		return SecretStoreFile
	}
	if b, err := os.ReadFile(secretStoreSettingPath()); err == nil {
		switch strings.TrimSpace(string(b)) {
		case SecretStoreFile:
			return SecretStoreFile
		case SecretStoreKeychain:
			return SecretStoreKeychain
		}
	}
	if testing.Testing() || runtime.GOOS != "darwin" {
		return SecretStoreFile
	}
	return SecretStoreKeychain
}

// UsesFileSecretStore is true when the OS keychain must not be touched.
func UsesFileSecretStore() bool { return SecretStore() == SecretStoreFile }

// KCGet reads a secret (service, account). An empty account matches the
// first item stored for service.
func KCGet(service, account string) (string, error) {
	if UsesFileSecretStore() {
		return fileGet(service, account)
	}
	return keychainGet(service, account)
}

// KCAccount returns the account attribute of the item stored for service.
func KCAccount(service string) string {
	if UsesFileSecretStore() {
		return fileAccount(service)
	}
	return keychainAccount(service)
}

// KCSet writes or updates a secret.
func KCSet(service, account, secret string) error {
	if UsesFileSecretStore() {
		return fileSet(service, account, secret)
	}
	return keychainSet(service, account, secret)
}

// ---------------------------------------------------------------------------
// Encrypted file vault
// ---------------------------------------------------------------------------

type vaultItem struct {
	Service string `json:"service"`
	Account string `json:"account"`
	Secret  string `json:"secret"`
}

var vaultMu sync.Mutex

const vaultMagic = "AMENC1:"

func loadVault() ([]vaultItem, error) {
	b, err := os.ReadFile(secretVaultPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSpace(b)
	if !bytes.HasPrefix(b, []byte(vaultMagic)) {
		return nil, fmt.Errorf("%s: unrecognised format", secretVaultPath())
	}
	enc, err := base64.StdEncoding.DecodeString(string(b[len(vaultMagic):]))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", secretVaultPath(), err)
	}
	plain, err := Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", secretVaultPath(), err)
	}
	var items []vaultItem
	if err := json.Unmarshal(plain, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", secretVaultPath(), err)
	}
	return items, nil
}

func saveVault(items []vaultItem) error {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Service != items[j].Service {
			return items[i].Service < items[j].Service
		}
		return items[i].Account < items[j].Account
	})
	plain, err := json.Marshal(items)
	if err != nil {
		return err
	}
	enc, err := Encrypt(plain)
	if err != nil {
		return err
	}
	return writeFileAtomic(secretVaultPath(), []byte(vaultMagic+base64.StdEncoding.EncodeToString(enc)+"\n"), 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func fileGet(service, account string) (string, error) {
	vaultMu.Lock()
	items, err := loadVault()
	vaultMu.Unlock()
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.Service == service && (account == "" || it.Account == account) {
			return it.Secret, nil
		}
	}
	// Claude Code keeps its own login in ~/.claude/.credentials.json when it
	// does not use the keychain (Linux, Windows, or keychain-less setups).
	if service == ClaudeKeychainService {
		if b, err := os.ReadFile(claudeCredentialsFile()); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return strings.TrimRight(string(b), "\n"), nil
		}
	}
	return "", fmt.Errorf("%w: %s/%s", errSecretNotFound, service, account)
}

func fileAccount(service string) string {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	items, _ := loadVault()
	for _, it := range items {
		if it.Service == service {
			return it.Account
		}
	}
	return ""
}

func fileSet(service, account, secret string) error {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	items, err := loadVault()
	if err != nil {
		return err
	}
	if account == "" {
		for _, it := range items {
			if it.Service == service {
				account = it.Account
				break
			}
		}
		if account == "" {
			account = types.CurrentUser()
		}
	}
	replaced := false
	for i := range items {
		if items[i].Service == service && items[i].Account == account {
			items[i].Secret = secret
			replaced = true
		}
	}
	if !replaced {
		items = append(items, vaultItem{Service: service, Account: account, Secret: secret})
	}
	if err := saveVault(items); err != nil {
		return err
	}
	if service == ClaudeKeychainService {
		// Mirror into Claude Code's own credential file so a keychain-less
		// Claude Code picks up rotations exactly as it would from the keychain.
		if err := writeFileAtomic(claudeCredentialsFile(), []byte(secret), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", claudeCredentialsFile(), err)
		}
	}
	return nil
}

func claudeCredentialsFile() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".credentials.json")
	}
	if testing.Testing() {
		return filepath.Join(types.BaseDir(), "claude-config", ".credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

// ---------------------------------------------------------------------------
// Switching backends
// ---------------------------------------------------------------------------

// knownKeychainItems are the items amux itself reads or writes.
func knownKeychainItems() [][2]string {
	return [][2]string{
		{ClaudeKeychainService, ""},
		{"gemini", "antigravity"},
		{"antigravity-service", "antigravity"},
	}
}

// SwitchResult reports what SetSecretStore carried over.
type SwitchResult struct {
	Mode           string   `json:"mode"`
	MasterKeyMoved bool     `json:"masterKeyMoved"`
	Copied         []string `json:"copied,omitempty"`
}

// SetSecretStore persists the backend choice. Switching to "file" first
// exports the keyring master key to ~/.amux/master.key (so AMENC1 files stay
// readable) and copies the keychain items amux uses into the vault — the
// last time the keychain is read. Switching back leaves the vault in place.
func SetSecretStore(mode string) (*SwitchResult, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != SecretStoreFile && mode != SecretStoreKeychain {
		return nil, fmt.Errorf("unknown secret store %q (use %q or %q)", mode, SecretStoreFile, SecretStoreKeychain)
	}
	res := &SwitchResult{Mode: mode}
	if mode == SecretStoreFile && SecretStore() == SecretStoreKeychain {
		if _, err := os.Stat(masterKeyFilePath()); os.IsNotExist(err) {
			if s, err := keyring.Get(keyringService, keyringUser); err == nil {
				if k, derr := base64.StdEncoding.DecodeString(s); derr == nil && len(k) == 32 {
					if err := writeFileAtomic(masterKeyFilePath(), []byte(s+"\n"), 0o600); err != nil {
						return nil, fmt.Errorf("export master key: %w", err)
					}
					res.MasterKeyMoved = true
				}
			}
		}
		var carry []vaultItem
		for _, it := range knownKeychainItems() {
			acct := it[1]
			if acct == "" {
				acct = keychainAccount(it[0])
			}
			if v, err := keychainGet(it[0], acct); err == nil && v != "" {
				carry = append(carry, vaultItem{Service: it[0], Account: acct, Secret: v})
			}
		}
		if err := writeFileAtomic(secretStoreSettingPath(), []byte(mode+"\n"), 0o600); err != nil {
			return nil, err
		}
		for _, it := range carry {
			if err := fileSet(it.Service, it.Account, it.Secret); err != nil {
				return res, fmt.Errorf("copy %s into vault: %w", it.Service, err)
			}
			res.Copied = append(res.Copied, it.Service)
		}
		return res, nil
	}
	return res, writeFileAtomic(secretStoreSettingPath(), []byte(mode+"\n"), 0o600)
}
