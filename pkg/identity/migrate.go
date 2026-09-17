package identity

import (
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
type LegacyAccountDoc struct {
	Providers []struct {
		ID           string  `json:"id"`
		Type         string  `json:"type"`
		Account      string  `json:"account"`
		Plan         string  `json:"plan"`
		Model        string  `json:"model"`
		Priority     int     `json:"priority"`
		Disabled     bool    `json:"disabled"`
		UsagePercent float64 `json:"usage_percent,omitempty"`
		ResetAt      int64   `json:"reset_at,omitempty"`
		ApiKey       string  `json:"api_key,omitempty"`
		RefreshToken string  `json:"refresh_token,omitempty"`
		SessionKey   string  `json:"session_key,omitempty"`
	} `json:"providers"`
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

				if p.ApiKey != "" {
					creds["api_key"] = p.ApiKey
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

				id := Identity{
					ID:           p.ID,
					Provider:     CanonicalProvider(p.Type),
					Tier:         tier,
					AuthType:     authType,
					Credentials:  creds,
					UsagePercent: p.UsagePercent,
					ResetAt:      p.ResetAt,
					Active:       !p.Disabled,
					Metadata: map[string]interface{}{
						"migrated_from": "accounts.json",
						"plan":          p.Plan,
						"model":         p.Model,
					},
				}
				cfg.Identities = append(cfg.Identities, id)
				existingMap[id.ID] = true
				migratedCount++
			}
		}
	}

	// 2. Read legacy profile store (Claude, Codex, Gemini profiles)
	profilesDir := filepath.Join(types.BaseDir(), "profiles")
	if _, err := os.Stat(profilesDir); os.IsNotExist(err) {
		home, _ := os.UserHomeDir()
		profilesDir = filepath.Join(home, ".am", "profiles")
	}
	tools := []string{"claude", "codex", "gemini"}
	for _, tool := range tools {
		dir := filepath.Join(profilesDir, tool)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, de := range entries {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".amux") {
				continue
			}
			name := strings.TrimSuffix(de.Name(), ".amux")
			id := fmt.Sprintf("%s-%s", tool, name)
			if existingMap[id] {
				continue
			}

			// Extract profile entries
			pe := profile.LoadProfileEntries(tool, name)
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

			meta := profile.ReadMeta(tool, name)
			idRecord := Identity{
				ID:           id,
				Provider:     CanonicalProvider(tool),
				Tier:         TierSubscription,
				AuthType:     string(AuthOAuth),
				Credentials:  creds,
				UsagePercent: 0.0,
				Active:       false,
				Metadata: map[string]interface{}{
					"migrated_from": "profile_bundle",
					"profile_name":  name,
					"email":         meta.Account,
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
