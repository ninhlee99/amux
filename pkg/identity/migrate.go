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

type accountKey struct {
	provider string
	email    string
}

func isNumericSuffixID(id string) bool {
	parts := strings.Split(id, ":")
	last := parts[len(parts)-1]
	if len(last) == 2 && last[0] >= '0' && last[0] <= '9' && last[1] >= '0' && last[1] <= '9' {
		return true
	}
	return false
}

// MigrateLegacyAccounts reads legacy accounts.json and profile bundles and returns migrated identities.
// It deduplicates accounts sharing the same provider and email (preventing named vs numeric duplicates like codex:ninhle21199 vs codex:01).
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

	// 0. Deduplicate any existing duplicates in cfg.Identities (same provider + email)
	// If one is named ID (e.g. codex:ninhle21199) and one is numeric (codex:01), merge credentials and retain the named ID.
	var cleaned []Identity
	seenAccts := make(map[accountKey]int)
	seenIDs := make(map[string]int)
	changed := false

	for _, id := range cfg.Identities {
		p := CanonicalProvider(id.Provider)
		em := strings.ToLower(strings.TrimSpace(id.Email()))
		if em == "" || em == "-" {
			if _, exists := seenIDs[id.ID]; !exists {
				seenIDs[id.ID] = len(cleaned)
				cleaned = append(cleaned, id)
			}
			continue
		}

		key := accountKey{provider: p, email: em}
		if existingIdx, exists := seenAccts[key]; exists {
			changed = true
			existing := &cleaned[existingIdx]
			if isNumericSuffixID(existing.ID) && !isNumericSuffixID(id.ID) {
				delete(seenIDs, existing.ID)
				existing.ID = id.ID
				seenIDs[id.ID] = existingIdx
			}
			if existing.Credentials == nil {
				existing.Credentials = make(map[string]string)
			}
			for k, v := range id.Credentials {
				if existing.Credentials[k] == "" && v != "" {
					existing.Credentials[k] = v
				}
			}
			if existing.Metadata == nil {
				existing.Metadata = make(map[string]interface{})
			}
			for k, v := range id.Metadata {
				if existing.Metadata[k] == nil && v != nil {
					existing.Metadata[k] = v
				}
			}
			if id.Active {
				existing.Active = true
			}
		} else {
			if _, exists := seenIDs[id.ID]; !exists {
				seenAccts[key] = len(cleaned)
				seenIDs[id.ID] = len(cleaned)
				cleaned = append(cleaned, id)
			}
		}
	}
	cfg.Identities = cleaned

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
				pEmail := strings.ToLower(strings.TrimSpace(p.Account))
				pProvider := CanonicalProvider(p.Type)
				key := accountKey{provider: pProvider, email: pEmail}

				tier := TierAPIKey
				authType := string(AuthAPIKey)
				creds := make(map[string]string)

				pType := strings.ToLower(p.Type)
				switch {
				case strings.Contains(pType, "sub") || types.IsSubscriptionTier(p.Plan) || pType == "codex_cli" || pType == "claude_oauth" || pType == "antigravity":
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
				if tier == TierSubscription || p.Disabled {
					meta["disabled"] = true
				}

				if pEmail != "" {
					if idx, exists := seenAccts[key]; exists {
						// 1 account per email per provider. Subscription strictly supersedes Web!
						if tier == TierSubscription && !cfg.Identities[idx].IsSubscription() {
							cfg.Identities[idx].ID = p.ID
							cfg.Identities[idx].Tier = TierSubscription
							cfg.Identities[idx].AuthType = authType
							cfg.Identities[idx].Active = false
							if cfg.Identities[idx].Metadata == nil {
								cfg.Identities[idx].Metadata = make(map[string]interface{})
							}
							cfg.Identities[idx].Metadata["disabled"] = true
							seenIDs[p.ID] = idx
						}
						enrichIdentityFromProvider(&cfg.Identities[idx], p)
						if cfg.Identities[idx].IsSubscription() || p.Disabled {
							cfg.Identities[idx].Active = false
							if cfg.Identities[idx].Metadata == nil {
								cfg.Identities[idx].Metadata = make(map[string]interface{})
							}
							cfg.Identities[idx].Metadata["disabled"] = true
						}
						continue
					}
				}

				if idx, exists := seenIDs[p.ID]; exists {
					enrichIdentityFromProvider(&cfg.Identities[idx], p)
					if cfg.Identities[idx].IsSubscription() || p.Disabled {
						cfg.Identities[idx].Active = false
						if cfg.Identities[idx].Metadata == nil {
							cfg.Identities[idx].Metadata = make(map[string]interface{})
						}
						cfg.Identities[idx].Metadata["disabled"] = true
					}
					continue
				}

				isActive := !p.Disabled
				if tier == TierSubscription {
					isActive = false
				}

				id := Identity{
					ID:           p.ID,
					Provider:     pProvider,
					Tier:         tier,
					AuthType:     authType,
					Credentials:  creds,
					UsagePercent: p.UsagePercent,
					ResetAt:      p.ResetAt,
					Active:       isActive,
					Metadata:     meta,
				}
				cfg.Identities = append(cfg.Identities, id)
				newIdx := len(cfg.Identities) - 1
				seenIDs[id.ID] = newIdx
				if pEmail != "" {
					seenAccts[key] = newIdx
				}
				migratedCount++
			}
		}
	}

	// 2. Read profile store (Claude, Codex, Gemini profiles).
	// Uses profile.ListProfiles which reads .meta.json files — profiles are stored
	// as .amp bundles.
	tools := []string{"claude", "codex", "gemini", "antigravity"}
	for _, tool := range tools {
		profiles := profile.ListProfiles(tool)
		for _, pm := range profiles {
			pmEmail := strings.ToLower(strings.TrimSpace(pm.Account))
			pProvider := CanonicalProvider(tool)
			key := accountKey{provider: pProvider, email: pmEmail}

			targetIdx := -1
			if idx, exists := seenIDs[pm.ID]; exists {
				targetIdx = idx
			} else if pmEmail != "" {
				if idx, exists := seenAccts[key]; exists {
					targetIdx = idx
				}
			}
			if targetIdx == -1 && pm.Name != "" {
				for i, id := range cfg.Identities {
					if CanonicalProvider(id.Provider) == pProvider && id.Metadata != nil {
						if prof, ok := id.Metadata["profile_name"].(string); ok && strings.EqualFold(prof, pm.Name) {
							targetIdx = i
							break
						}
					}
				}
			}

			bundleTier := TierSubscription
			bundleAuth := string(AuthOAuth)
			if strings.Contains(pm.ID, ":web:") || strings.EqualFold(fmt.Sprint(pm.Plan), "free") {
				bundleTier = TierWeb
				bundleAuth = string(AuthCDP)
			}

			if targetIdx != -1 {
				if bundleTier == TierSubscription && !cfg.Identities[targetIdx].IsSubscription() {
					// Subscription strictly supersedes Web
					cfg.Identities[targetIdx].ID = pm.ID
					cfg.Identities[targetIdx].Tier = TierSubscription
					cfg.Identities[targetIdx].AuthType = bundleAuth
					cfg.Identities[targetIdx].Active = false
					if cfg.Identities[targetIdx].Metadata == nil {
						cfg.Identities[targetIdx].Metadata = make(map[string]interface{})
					}
					cfg.Identities[targetIdx].Metadata["disabled"] = true
					seenIDs[pm.ID] = targetIdx
				}
				pe := profile.LoadProfileEntries(tool, pm.Name)
				enrichIdentityFromProfile(&cfg.Identities[targetIdx], tool, pm, pe)
				if cfg.Identities[targetIdx].IsSubscription() || pm.Disabled {
					cfg.Identities[targetIdx].Active = false
					if cfg.Identities[targetIdx].Metadata == nil {
						cfg.Identities[targetIdx].Metadata = make(map[string]interface{})
					}
					cfg.Identities[targetIdx].Metadata["disabled"] = true
				}
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
			isActive := activeName == pm.Name && !pm.Disabled && bundleTier != TierSubscription

			meta := map[string]interface{}{
				"migrated_from": "profile_bundle",
				"profile_name":  pm.Name,
				"email":         pm.Account,
				"plan":          pm.Plan,
			}
			if bundleTier == TierSubscription || pm.Disabled {
				isActive = false
				meta["disabled"] = true
			}

			idRecord := Identity{
				ID:           pm.ID,
				Provider:     pProvider,
				Tier:         bundleTier,
				AuthType:     bundleAuth,
				Credentials:  creds,
				UsagePercent: 0.0,
				Active:       isActive,
				Metadata:     meta,
			}
			cfg.Identities = append(cfg.Identities, idRecord)
			newIdx := len(cfg.Identities) - 1
			seenIDs[idRecord.ID] = newIdx
			if pmEmail != "" {
				seenAccts[key] = newIdx
			}
			migratedCount++
		}
	}

	cfg.Identities = DeduplicateIdentities(cfg.Identities)
	if migratedCount > 0 || changed {
		if err := SaveConfig(identitiesPath, cfg); err != nil {
			return 0, fmt.Errorf("save migrated config: %w", err)
		}
	}

	return migratedCount, nil
}

func enrichIdentityFromProvider(target *Identity, p LegacyProvider) {
	if target.Credentials == nil {
		target.Credentials = make(map[string]string)
	}
	apiKey := p.ApiKey
	if apiKey == "" {
		apiKey = p.ApiKeySnake
	}
	if apiKey != "" && target.Credentials["api_key"] == "" {
		target.Credentials["api_key"] = apiKey
	}
	if p.BaseURL != "" && target.Credentials["base_url"] == "" {
		target.Credentials["base_url"] = p.BaseURL
	}
	if p.RefreshToken != "" && target.Credentials["refresh_token"] == "" {
		target.Credentials["refresh_token"] = p.RefreshToken
	}
	if p.SessionKey != "" && target.Credentials["session_key"] == "" {
		target.Credentials["session_key"] = p.SessionKey
	}
	if p.Account != "" && target.Credentials["account"] == "" {
		target.Credentials["account"] = p.Account
	}
	if target.Metadata == nil {
		target.Metadata = make(map[string]interface{})
	}
	if p.Plan != "" && (target.Metadata["plan"] == nil || target.Metadata["plan"] == "") {
		target.Metadata["plan"] = p.Plan
	}
	if p.Model != "" && (target.Metadata["model"] == nil || target.Metadata["model"] == "") {
		target.Metadata["model"] = p.Model
	}
	if p.BaseURL != "" && target.Metadata["endpoint"] == nil {
		target.Metadata["endpoint"] = p.BaseURL
	}
}

func enrichIdentityFromProfile(target *Identity, tool string, pm types.ProfileMeta, pe []types.ProfileEntry) {
	if target.Credentials == nil {
		target.Credentials = make(map[string]string)
	}
	for _, entry := range pe {
		if len(entry.Data) > 0 {
			if entry.Artifact.Kind == "keychain" {
				tok := auth.ParseClaudeCreds(entry.Data)
				if tok != nil {
					if target.Credentials["access_token"] == "" {
						target.Credentials["access_token"] = tok.Access
					}
					if target.Credentials["refresh_token"] == "" {
						target.Credentials["refresh_token"] = tok.Refresh
					}
				} else {
					if target.Credentials["keychain_data"] == "" {
						target.Credentials["keychain_data"] = string(entry.Data)
					}
				}
			} else {
				if target.Credentials["file_data"] == "" {
					target.Credentials["file_data"] = string(entry.Data)
				}
			}
		}
	}
	if target.Metadata == nil {
		target.Metadata = make(map[string]interface{})
	}
	if pm.Name != "" {
		target.Metadata["profile_name"] = pm.Name
	}
	if pm.Account != "" && (target.Metadata["email"] == nil || target.Metadata["email"] == "") {
		target.Metadata["email"] = pm.Account
	}
	if pm.Plan != "" && (target.Metadata["plan"] == nil || target.Metadata["plan"] == "") {
		target.Metadata["plan"] = pm.Plan
	}
	activeName := profile.ReadActivePointer(tool)
	if activeName == pm.Name && !pm.Disabled && target.Tier != TierSubscription {
		target.Active = true
	}
	if pm.Disabled || target.Tier == TierSubscription {
		target.Active = false
		target.Metadata["disabled"] = true
	}
}
