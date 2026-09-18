package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/types"
)

// LegacyAccountDoc represents ~/.am/accounts.json legacy structure.
// LegacyProvider represents a provider entry in ~/.am/accounts.json.
type LegacyProvider struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Account      string  `json:"account"`
	Plan         string  `json:"plan"`
	Model        string  `json:"model"`
	BaseURL      string  `json:"baseUrl,omitempty"`
	Priority     int     `json:"priority"`
	Disabled     bool    `json:"disabled"`
	UsagePercent float64 `json:"usage_percent,omitempty"`
	ResetAt      int64   `json:"reset_at,omitempty"`
	ApiKey       string  `json:"apiKey,omitempty"`
	ApiKeySnake  string  `json:"api_key,omitempty"`
	RefreshToken string  `json:"refresh_token,omitempty"`
	SessionKey   string  `json:"session_key,omitempty"`
}

// LegacyAccountDoc represents ~/.am/accounts.json legacy structure.
type LegacyAccountDoc struct {
	Providers []LegacyProvider `json:"providers"`
}

// MigrateLegacyAccounts reads legacy accounts.json and profile bundles and returns migrated identities.
// It never deletes or alters legacy files.
func MigrateLegacyAccounts(accountsPath string, identitiesPath string) (int, error) {
	if accountsPath == "" {
		accountsPath = filepath.Join(types.BaseDir(), "accounts.json")
		if _, err := os.Stat(accountsPath); os.IsNotExist(err) {
			home, _ := os.UserHomeDir()
			accountsPath = filepath.Join(home, ".am", "accounts.json")
		}
	}
	if identitiesPath == "" {
		identitiesPath = DefaultIdentitiesPath()
	}

	cfg, err := LoadConfig(identitiesPath)
	if err != nil {
		cfg = &Config{ThresholdPct: DefaultThresholdPct, Identities: []Identity{}}
	}

	existingMap := make(map[string]bool)
	for _, id := range cfg.Identities {
		existingMap[id.ID] = true
	}

	migratedCount := 0

	// 1. Read accounts.json
	data, err := os.ReadFile(accountsPath)
	if err == nil {
		if bytes.HasPrefix(data, []byte("AMENC1:")) {
			enc := data[len("AMENC1:"):]
			if dec, derr := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(enc))); derr == nil {
				if plain, perr := auth.Decrypt(dec); perr == nil {
					data = plain
				}
			}
		}
		var doc LegacyAccountDoc
		if json.Unmarshal(data, &doc) == nil {
			for _, p := range doc.Providers {
				if existingMap[p.ID] {
					continue
				}

				tier := TierAPIKey
				authType := string(AuthAPIKey)
				creds := make(map[string]string)

				pType := strings.ToLower(p.Type)
				switch {
				case strings.Contains(pType, "sub") || types.IsSubscriptionTier(p.Plan) || pType == "codex_cli" || pType == "claude_oauth":
					tier = TierSubscription
					authType = string(AuthOAuth)
				case strings.Contains(pType, "web"):
					tier = TierWeb
					authType = string(AuthCDP)
				default:
					tier = TierAPIKey
					authType = string(AuthAPIKey)
				}

				apiKey := p.ApiKey
				if apiKey == "" {
					apiKey = p.ApiKeySnake
				}
				if apiKey != "" {
					creds["api_key"] = apiKey
				}
				if p.BaseURL != "" {
					creds["base_url"] = p.BaseURL
				}
				if p.RefreshToken != "" {
					creds["refresh_token"] = p.RefreshToken
				}
				if p.SessionKey != "" {
					creds["session_key"] = p.SessionKey
				}
				if p.Account != "" {
					creds["account"] = p.Account
				}

				meta := map[string]interface{}{
					"migrated_from": "accounts.json",
					"plan":          p.Plan,
					"model":         p.Model,
				}
				if p.BaseURL != "" {
					meta["endpoint"] = p.BaseURL
				}

				id := Identity{
					ID:           p.ID,
					Provider:     CanonicalProvider(p.Type),
					Tier:         tier,
					AuthType:     authType,
					Credentials:  creds,
					UsagePercent: p.UsagePercent,
					ResetAt:      p.ResetAt,
					Active:       !p.Disabled,
					Metadata:     meta,
				}
				cfg.Identities = append(cfg.Identities, id)
				existingMap[id.ID] = true
				migratedCount++
			}
		}
	}

	// 2. Read profile store (Claude, Codex, Gemini profiles).
	// Uses profile.ListProfiles which reads .meta.json files — profiles are stored
	// as .amp bundles (not .amux). IDs are generated consistently with profile.IDPrefixForTool.
	tools := []string{"claude", "codex", "gemini", "antigravity"}
	for _, tool := range tools {
		profiles := profile.ListProfiles(tool)
		for _, pm := range profiles {
			// pm.ID is set by ListProfiles using types.FormatID(prefix, i+1)
			// e.g. "claude:code:01", "codex:01"
			id := pm.ID
			if existingMap[id] {
				continue
			}

			// Extract credentials from the profile bundle
			pe := profile.LoadProfileEntries(tool, pm.Name)
			creds := make(map[string]string)
			for _, entry := range pe {
				if len(entry.Data) > 0 {
					if entry.Artifact.Kind == "keychain" {
						tok := auth.ParseClaudeCreds(entry.Data)
						if tok != nil {
							creds["access_token"] = tok.Access
							creds["refresh_token"] = tok.Refresh
						} else {
							creds["keychain_data"] = string(entry.Data)
						}
					} else {
						creds["file_data"] = string(entry.Data)
					}
				}
			}

			// Determine active status from the profile's .active pointer
			activeName := profile.ReadActivePointer(tool)
			isActive := activeName == pm.Name && !pm.Disabled

			idRecord := Identity{
				ID:           id,
				Provider:     CanonicalProvider(tool),
				Tier:         TierSubscription,
				AuthType:     string(AuthOAuth),
				Credentials:  creds,
				UsagePercent: 0.0,
				Active:       isActive,
				Metadata: map[string]interface{}{
					"migrated_from": "profile_bundle",
					"profile_name":  pm.Name,
					"email":         pm.Account,
					"plan":          pm.Plan,
				},
			}
			cfg.Identities = append(cfg.Identities, idRecord)
			existingMap[id] = true
			migratedCount++
		}
	}

	if migratedCount > 0 {
		if err := SaveConfig(identitiesPath, cfg); err != nil {
			return 0, fmt.Errorf("save migrated config: %w", err)
		}
	}

	return migratedCount, nil
}
