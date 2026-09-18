package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/types"
)

const (
	ClaudeKeychainService = "Claude Code-credentials"
)

// SyncIdentityToNativeKeychain writes an identity's active credentials to the native IDE's OS Keychain or config file.
// This allows the IDE (e.g. Claude Code, Codex) to execute natively without proxy intervention.
func SyncIdentityToNativeKeychain(id *Identity) error {
	if id == nil {
		return fmt.Errorf("nil identity")
	}

	canon := CanonicalProvider(id.Provider)
	switch canon {
	case "anthropic":
		return syncClaudeKeychain(id)
	case "openai":
		return syncCodexAuth(id)
	case "gemini":
		return syncGeminiAuth(id)
	default:
		return nil
	}
}

func syncClaudeKeychain(id *Identity) error {
	accessTok := id.Credentials["access_token"]
	refreshTok := id.Credentials["refresh_token"]
	if accessTok == "" && refreshTok == "" {
		return fmt.Errorf("no tokens available for identity %s", id.ID)
	}

	var expiresAt int64
	if id.ResetAt > 0 {
		expiresAt = id.ResetAt * 1000
	} else {
		expiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	}

	payload := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      accessTok,
			"refreshToken":     refreshTok,
			"expiresAt":        expiresAt,
			"scopes":           []string{"user:read", "user:write"},
			"subscriptionType": "pro",
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal claude creds: %w", err)
	}

	acct := types.CurrentUser()
	if err := auth.KCSet(ClaudeKeychainService, acct, string(b)); err != nil {
		return err
	}

	if email := id.Email(); email != "" && email != "-" {
		home, err := os.UserHomeDir()
		if err == nil {
			claudeJSONPath := filepath.Join(home, ".claude.json")
			if data, err := os.ReadFile(claudeJSONPath); err == nil {
				var doc map[string]any
				if json.Unmarshal(data, &doc) == nil {
					if oauthAcct, ok := doc["oauthAccount"].(map[string]any); ok {
						oauthAcct["emailAddress"] = email
						if nb, err := json.MarshalIndent(doc, "", "  "); err == nil {
							_ = os.WriteFile(claudeJSONPath, nb, 0o600)
						}
					}
				}
			}
		}
	}
	return nil
}

func syncCodexAuth(id *Identity) error {
	accessTok := id.Credentials["access_token"]
	refreshTok := id.Credentials["refresh_token"]
	if accessTok == "" && refreshTok == "" {
		return fmt.Errorf("no tokens for codex identity %s", id.ID)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	_ = os.MkdirAll(filepath.Dir(authPath), 0o700)

	doc := map[string]any{
		"tokens": map[string]any{
			"access_token":  accessTok,
			"refresh_token": refreshTok,
			"id_token":      id.Credentials["id_token"],
			"account_id":    id.Credentials["account_id"],
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(authPath, append(b, '\n'), 0o600)
}

func syncGeminiAuth(id *Identity) error {
	keychainData := id.Credentials["keychain_data"]
	email := id.Email()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	if keychainData != "" {
		_ = auth.KCSet("gemini", "antigravity", keychainData)
		if email != "" && email != "-" {
			_ = auth.KCSet("antigravity-service", email, keychainData)
			_ = auth.KCSet("antigravity-service", "antigravity", keychainData)
		}
	}

	if email != "" && email != "-" {
		googleAcctsPath := filepath.Join(home, ".gemini", "google_accounts.json")
		var gAccts struct {
			Active string   `json:"active"`
			Old    []string `json:"old"`
		}
		if d, err := os.ReadFile(googleAcctsPath); err == nil {
			_ = json.Unmarshal(d, &gAccts)
		}
		gAccts.Active = email
		found := false
		for _, o := range gAccts.Old {
			if o == email {
				found = true
				break
			}
		}
		if !found {
			gAccts.Old = append(gAccts.Old, email)
		}
		if gAcctsBytes, err := json.MarshalIndent(gAccts, "", "  "); err == nil {
			_ = os.WriteFile(googleAcctsPath, gAcctsBytes, 0o600)
		}
	}
	return nil
}

// RotateSubscriptionKeychain performs silent Keychain rotation when the active subscription hits threshold.
// It locates the next available subscription under threshold and writes it to the native Keychain.
func RotateSubscriptionKeychain(provider string, path string) (*Identity, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	canon := CanonicalProvider(provider)
	var activeID string
	for _, id := range cfg.Identities {
		if CanonicalProvider(id.Provider) == canon && id.Active && id.IsSubscription() {
			activeID = id.ID
			break
		}
	}

	next, ok := NextSubscription(provider, activeID, cfg.Identities, cfg.ThresholdPct)
	if !ok {
		return nil, fmt.Errorf("no alternative subscription account under threshold for %s", provider)
	}

	// Update active flags
	if err := SetActive(path, next.ID); err != nil {
		return nil, fmt.Errorf("activate identity: %w", err)
	}

	// Silently sync to native Keychain
	if err := SyncIdentityToNativeKeychain(next); err != nil {
		return nil, fmt.Errorf("sync to keychain: %w", err)
	}

	return next, nil
}
