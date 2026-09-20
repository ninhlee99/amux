package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"amux-accounts/pkg/types"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "am-master-key"
	keyringUser    = "default"
)

// MasterKey returns the 32-byte AES key, creating and storing one in the
// system keychain (or ~/.amux/master.key file fallback when keychain/D-Bus is unavailable) on first use.
func MasterKey() ([]byte, error) {
	if envKey := os.Getenv("AMUX_MASTER_KEY"); envKey != "" {
		if k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey)); err == nil && len(k) == 32 {
			return k, nil
		}
	}

	// 1. Try system keyring
	s, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		k, decErr := base64.StdEncoding.DecodeString(s)
		if decErr == nil && len(k) == 32 {
			return k, nil
		}
		return nil, fmt.Errorf("stored master key is corrupt: %w", decErr)
	}

	// 2. Check local fallback file
	keyFilePath := filepath.Join(types.BaseDir(), "master.key")
	if fileBytes, errFile := os.ReadFile(keyFilePath); errFile == nil {
		k, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fileBytes)))
		if decErr == nil && len(k) == 32 {
			return k, nil
		}
	}

	// If keyring errored with something other than ErrNotFound, but file also doesn't exist,
	// we will generate a key and persist to file or keyring.
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("rand: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(k)

	// Try keyring first
	if setErr := keyring.Set(keyringService, keyringUser, encoded); setErr == nil {
		return k, nil
	}

	// Fallback to file storage if keyring is not available
	_ = os.MkdirAll(types.BaseDir(), 0o700)
	if errFile := os.WriteFile(keyFilePath, []byte(encoded+"\n"), 0o600); errFile != nil {
		if !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("store master key in keychain failed (%v) and fallback file failed: %w", err, errFile)
		}
		return nil, fmt.Errorf("store master key: %w", errFile)
	}

	return k, nil
}

// Encrypt encrypts plain bytes using AES-GCM with the master key.
func Encrypt(plain []byte) ([]byte, error) {
	key, err := MasterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// Decrypt decrypts encrypted bytes using AES-GCM with the master key.
func Decrypt(enc []byte) ([]byte, error) {
	key, err := MasterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	if len(enc) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, body := enc[:gcm.NonceSize()], enc[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt (wrong master key?): %w", err)
	}
	return out, nil
}
