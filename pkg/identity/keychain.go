package identity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
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
		if id.Metadata != nil {
			if pName, ok := id.Metadata["profile_name"].(string); ok && pName != "" {
				tok := profile.LoadClaudeToken("claude", pName)
				if tok != nil && (tok.Access != "" || tok.Refresh != "") {
					accessTok = tok.Access
					refreshTok = tok.Refresh
				}
			}
		}
	}

	if accessTok == "" && refreshTok == "" {
		if kcData, ok := id.Credentials["keychain_data"]; ok && kcData != "" {
			tok := auth.ParseClaudeCreds([]byte(kcData))
			if tok != nil && (tok.Access != "" || tok.Refresh != "") {
				accessTok = tok.Access
				refreshTok = tok.Refresh
			}
		}
	}

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
	_ = auth.KCSet(ClaudeKeychainService, acct, string(b))

	email := id.Email()
	if email != "" && email != "-" {
		_ = auth.KCSet(ClaudeKeychainService, email, string(b))
		home, err := os.UserHomeDir()
		if err == nil {
			claudeJSONPath := filepath.Join(home, ".claude.json")
			var doc map[string]any
			if data, err := os.ReadFile(claudeJSONPath); err == nil {
				_ = json.Unmarshal(data, &doc)
			}
			if doc == nil {
				doc = make(map[string]any)
			}
			oauthAcct, _ := doc["oauthAccount"].(map[string]any)
			if oauthAcct == nil {
				oauthAcct = make(map[string]any)
			}
			oauthAcct["emailAddress"] = email
			doc["oauthAccount"] = oauthAcct
			if nb, err := json.MarshalIndent(doc, "", "  "); err == nil {
				_ = os.WriteFile(claudeJSONPath, nb, 0o600)
			}
		}
	}
	return nil
}

func syncCodexAuth(id *Identity) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	_ = os.MkdirAll(filepath.Dir(authPath), 0o700)

	fileData := id.Credentials["file_data"]
	accessTok := id.Credentials["access_token"]
	refreshTok := id.Credentials["refresh_token"]

	if accessTok == "" && refreshTok == "" && fileData != "" {
		return os.WriteFile(authPath, []byte(fileData), 0o600)
	}

	if accessTok == "" && refreshTok == "" {
		return fmt.Errorf("no tokens for codex identity %s", id.ID)
	}

	tokensMap := map[string]any{
		"access_token":  accessTok,
		"refresh_token": refreshTok,
	}
	if idTok, ok := id.Credentials["id_token"]; ok && idTok != "" {
		tokensMap["id_token"] = idTok
	}
	if acctID, ok := id.Credentials["account_id"]; ok && acctID != "" {
		tokensMap["account_id"] = acctID
	}

	doc := map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens":         tokensMap,
		"last_refresh":   time.Now().UTC().Format(time.RFC3339),
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

		// Also restore ~/.gemini/antigravity-cli/credentials.json so that AGY CLI
		// can start in native mode (without going through the proxy gateway).
		// The keychain_data is stored as "go-keyring-base64:<base64(JSON)>" where
		// JSON = {token:{access_token,refresh_token,expiry,...}, auth_method, id_token}.
		_ = writeAgyCredentialsJSON(home, keychainData, email)
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

// writeAgyCredentialsJSON writes ~/.gemini/antigravity-cli/credentials.json from
// the stored keychain_data value (which is in "go-keyring-base64:<b64>" format).
// AGY CLI reads this file on startup to authenticate without hitting the gateway.
func writeAgyCredentialsJSON(home, keychainData, email string) error {
	raw := keychainData
	if strings.HasPrefix(raw, "go-keyring-base64:") {
		decoded, err := base64.StdEncoding.DecodeString(raw[len("go-keyring-base64:"):])
		if err != nil {
			return err
		}
		raw = string(decoded)
	}

	var kd struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Expiry       string `json:"expiry"`
		} `json:"token"`
		AuthMethod string `json:"auth_method"`
		IDToken    string `json:"id_token"`
	}
	if err := json.Unmarshal([]byte(raw), &kd); err != nil {
		return err
	}

	if kd.Token.AccessToken == "" && kd.Token.RefreshToken == "" {
		return nil // nothing to write
	}

	// Compute expires_at in milliseconds (same as OAuth login flow).
	var expiresAt int64
	if kd.Token.Expiry != "" {
		if t, err := time.Parse(time.RFC3339Nano, kd.Token.Expiry); err == nil {
			expiresAt = t.UnixMilli()
		}
	}
	if expiresAt == 0 {
		expiresAt = time.Now().Add(time.Hour).UnixMilli()
	}

	// Parse email from id_token if not provided.
	if email == "" || email == "-" {
		email = parseJWTEmailLocal(kd.IDToken)
	}

	credsPayload, err := json.MarshalIndent(map[string]any{
		"access_token":  kd.Token.AccessToken,
		"refresh_token": kd.Token.RefreshToken,
		"id_token":      kd.IDToken,
		"email":         email,
		"expires_at":    expiresAt,
	}, "", "  ")
	if err != nil {
		return err
	}

	agyDir := filepath.Join(home, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(agyDir, 0o700)
	return os.WriteFile(filepath.Join(agyDir, "credentials.json"), credsPayload, 0o600)
}

// parseJWTEmailLocal extracts the email claim from a JWT id_token without verifying signature.
func parseJWTEmailLocal(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	// Add padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	email, _ := claims["email"].(string)
	return email
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
