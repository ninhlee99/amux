package identity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
)

func TestKeychainRotation_MultiAccountThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-keychain-rot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "identities.json")

	cfg := &identity.Config{
		ThresholdPct: 95.0,
		Identities: []identity.Identity{
			{
				ID:           "claude-sub-1",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				AuthType:     string(identity.AuthOAuth),
				Credentials:  map[string]string{"access_token": "token-sub-1"},
				UsagePercent: 96.0, // Over 95% threshold
				Active:       true,
			},
			{
				ID:           "claude-sub-2",
				Provider:     "anthropic",
				Tier:         identity.TierSubscription,
				AuthType:     string(identity.AuthOAuth),
				Credentials:  map[string]string{"access_token": "token-sub-2"},
				UsagePercent: 20.0, // Healthy alternative
				Active:       false,
			},
		},
	}
	if err := identity.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	// Active account claude-sub-1 is at 96% in a multi-account pool -> should failover
	if !identity.ShouldFailover(cfg.Identities[0], cfg.Identities, cfg.ThresholdPct) {
		t.Errorf("claude-sub-1 should trigger failover at 96%%")
	}

	// Locate next subscription
	next, ok := identity.NextSubscription("anthropic", "claude-sub-1", cfg.Identities, cfg.ThresholdPct)
	if !ok || next == nil || next.ID != "claude-sub-2" {
		t.Fatalf("expected claude-sub-2 as next candidate, got %v", next)
	}

	// Perform rotation
	if err := identity.SetActive(cfgPath, next.ID); err != nil {
		t.Fatalf("SetActive error: %v", err)
	}

	reloaded, err := identity.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range reloaded.Identities {
		if id.ID == "claude-sub-2" && !id.Active {
			t.Errorf("expected claude-sub-2 to be active")
		}
		if id.ID == "claude-sub-1" && id.Active {
			t.Errorf("expected claude-sub-1 to be inactive")
		}
	}
}

func TestKeychainRotation_SingleAccountLimit(t *testing.T) {
	// A single-account pool allows usage up to 100%
	singlePool := []identity.Identity{
		{
			ID:           "solo-sub",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 99.0,
			Active:       true,
		},
	}

	eff := identity.GetEffectiveThreshold("anthropic", singlePool, 95.0)
	if eff != 100.0 {
		t.Fatalf("single account effective threshold should be 100.0, got %f", eff)
	}

	if identity.ShouldFailover(singlePool[0], singlePool, 95.0) {
		t.Errorf("solo subscription at 99%% should NOT failover under 100%% rule")
	}

	singlePool[0].UsagePercent = 100.0
	if !identity.ShouldFailover(singlePool[0], singlePool, 95.0) {
		t.Errorf("solo subscription at 100%% should failover")
	}
}

func TestMigration_LegacyDataPreservation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-migration-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	legacyAccountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	legacyDoc := identity.LegacyAccountDoc{
		Providers: []struct {
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
		}{
			{
				ID:           "claude-pro-1",
				Type:         "claude_oauth",
				Account:      "user@example.com",
				Plan:         "pro",
				RefreshToken: "refresh-12345",
				UsagePercent: 45.0,
				Disabled:     false,
			},
			{
				ID:         "chatgpt-free-1",
				Type:       "chatgpt_web",
				Account:    "free@example.com",
				SessionKey: "sess-abcde",
				Disabled:   false,
			},
			{
				ID:       "custom-api-key",
				Type:     "api_other",
				ApiKey:   "sk-ant-testkey123",
				Disabled: true,
			},
		},
	}
	b, _ := json.MarshalIndent(legacyDoc, "", "  ")
	if err := os.WriteFile(legacyAccountsPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	migratedCount, err := identity.MigrateLegacyAccounts(legacyAccountsPath, identitiesPath)
	if err != nil {
		t.Fatalf("MigrateLegacyAccounts failed: %v", err)
	}
	if migratedCount != 3 {
		t.Fatalf("expected 3 migrated accounts, got %d", migratedCount)
	}

	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Identities) != 3 {
		t.Fatalf("expected 3 identities in config, got %d", len(cfg.Identities))
	}

	// Verify claude-pro-1 mapped to subscription tier with refresh token
	var claudeID *identity.Identity
	for _, id := range cfg.Identities {
		if id.ID == "claude-pro-1" {
			claudeID = &id
			break
		}
	}
	if claudeID == nil {
		t.Fatal("claude-pro-1 missing from migrated identities")
	}
	if claudeID.Tier != identity.TierSubscription {
		t.Errorf("expected claude-pro-1 tier subscription, got %s", claudeID.Tier)
	}
	if claudeID.Credentials["refresh_token"] != "refresh-12345" {
		t.Errorf("refresh token was not preserved")
	}

	// Re-running migration should be idempotent (0 new migrations)
	secondCount, err := identity.MigrateLegacyAccounts(legacyAccountsPath, identitiesPath)
	if err != nil || secondCount != 0 {
		t.Errorf("expected 0 migrated on second run (idempotent), got %d (err: %v)", secondCount, err)
	}
}
