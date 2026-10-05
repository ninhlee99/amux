package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AMUX_HOME", dir)
	t.Setenv("AMUX_SECRET_STORE", "")
	t.Setenv("AMUX_NO_KEYCHAIN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("AMUX_MASTER_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	return dir
}

func TestSecretStore_DefaultsToFileUnderTest(t *testing.T) {
	isolate(t)
	if SecretStore() != SecretStoreFile {
		t.Fatalf("test binaries must never use the OS keychain, got %s", SecretStore())
	}
	t.Setenv("AMUX_SECRET_STORE", "keychain")
	if SecretStore() != SecretStoreKeychain {
		t.Fatal("explicit env override must win")
	}
}

func TestFileVault_RoundTripEncrypted(t *testing.T) {
	dir := isolate(t)
	if err := KCSet("svc", "alice", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := KCSet("svc", "bob", "other"); err != nil {
		t.Fatal(err)
	}
	if err := KCSet("svc", "alice", "rotated"); err != nil {
		t.Fatal(err)
	}
	if v, err := KCGet("svc", "alice"); err != nil || v != "rotated" {
		t.Fatalf("KCGet = %q, %v", v, err)
	}
	if v, _ := KCGet("svc", ""); v == "" {
		t.Fatal("empty account should match the first item")
	}
	if KCAccount("svc") == "" {
		t.Fatal("KCAccount empty")
	}
	if _, err := KCGet("nope", ""); err == nil {
		t.Fatal("expected not-found")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "secrets.vault"))
	if !strings.HasPrefix(string(raw), "AMENC1:") || strings.Contains(string(raw), "rotated") {
		t.Fatalf("vault must be encrypted at rest: %q", raw)
	}
	if st, _ := os.Stat(filepath.Join(dir, "secrets.vault")); st.Mode().Perm() != 0o600 {
		t.Fatalf("vault perms = %v", st.Mode().Perm())
	}
}

func TestFileVault_ClaudeCredentialsMirror(t *testing.T) {
	dir := isolate(t)
	creds := `{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt","expiresAt":1}}`
	if err := KCSet(ClaudeKeychainService, "", creds); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "claude", ".credentials.json"))
	if err != nil || string(b) != creds {
		t.Fatalf("mirror = %q, %v", b, err)
	}
	// A token Claude Code wrote itself is readable even if the vault lacks it.
	_ = os.Remove(filepath.Join(dir, "secrets.vault"))
	if tok := LiveKeychainToken(); tok == nil || tok.Access != "at" {
		t.Fatalf("LiveKeychainToken from file = %+v", tok)
	}
}

func TestSetSecretStore_Persists(t *testing.T) {
	dir := isolate(t)
	if _, err := SetSecretStore("bogus"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := SetSecretStore("keychain"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "secret_store")); strings.TrimSpace(string(b)) != "keychain" {
		t.Fatalf("setting = %q", b)
	}
	if SecretStore() != SecretStoreKeychain {
		t.Fatal("persisted setting should apply")
	}
}
