package identity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/types"
)

func TestMigrateLegacyAccounts_NoDuplicatesBetweenAccountsAndProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)

	accountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	// 1. Setup accounts.json with named ID codex:ninhle21199
	doc := identity.LegacyAccountDoc{
		Providers: []identity.LegacyProvider{
			{
				ID:           "codex:ninhle21199",
				Type:         "codex_cli",
				Account:      "ninhle21199@gmail.com",
				Plan:         "pro",
				RefreshToken: "refresh-codex-123",
			},
		},
	}
	docBytes, _ := json.Marshal(doc)
	_ = os.WriteFile(accountsPath, docBytes, 0o600)

	// 2. Setup profile bundle for codex with same email
	profDir := filepath.Join(tmpDir, "profiles", "codex")
	_ = os.MkdirAll(profDir, 0o700)
	meta := types.ProfileMeta{
		Name:    "ninhle21199@gmail.com",
		Tool:    "codex",
		Account: "ninhle21199@gmail.com",
		Plan:    "pro",
	}
	metaBytes, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(profDir, "ninhle21199@gmail.com.meta.json"), metaBytes, 0o600)
	_ = os.WriteFile(filepath.Join(profDir, "ninhle21199@gmail.com.amp"), []byte("dummy-bundle"), 0o600)

	// 3. Run migration
	migrated, err := identity.MigrateLegacyAccounts(accountsPath, identitiesPath)
	if err != nil {
		t.Fatalf("MigrateLegacyAccounts failed: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("expected 1 migrated account, got %d", migrated)
	}

	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Identities) != 1 {
		var ids []string
		for _, id := range cfg.Identities {
			ids = append(ids, id.ID)
		}
		t.Fatalf("expected exactly 1 identity, got %d: %v", len(cfg.Identities), ids)
	}

	if cfg.Identities[0].ID != "codex:ninhle21199" {
		t.Errorf("expected identity ID 'codex:ninhle21199', got %q", cfg.Identities[0].ID)
	}

	// 4. Test deduplication when duplicates already exist in identities.json
	cfg.Identities = append(cfg.Identities, identity.Identity{
		ID:       "codex:01",
		Provider: "openai",
		Tier:     identity.TierSubscription,
		AuthType: string(identity.AuthOAuth),
		Metadata: map[string]interface{}{
			"email": "ninhle21199@gmail.com",
		},
	})
	_ = identity.SaveConfig(identitiesPath, cfg)

	// Re-run migration; should clean up the duplicate codex:01 and keep codex:ninhle21199
	_, err = identity.MigrateLegacyAccounts(accountsPath, identitiesPath)
	if err != nil {
		t.Fatalf("second MigrateLegacyAccounts failed: %v", err)
	}

	cfg2, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatalf("LoadConfig 2 failed: %v", err)
	}
	if len(cfg2.Identities) != 1 {
		var ids []string
		for _, id := range cfg2.Identities {
			ids = append(ids, id.ID)
		}
		t.Fatalf("expected duplicates to be deduplicated to 1 identity, got %d: %v", len(cfg2.Identities), ids)
	}
	if cfg2.Identities[0].ID != "codex:ninhle21199" {
		t.Errorf("expected named ID 'codex:ninhle21199' to be preserved, got %q", cfg2.Identities[0].ID)
	}
}

func TestMigrateLegacyAccounts_SubBeatsWebForSameEmail(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)

	accountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	// In accounts.json: web account comes first, subscription comes second for same email
	doc := identity.LegacyAccountDoc{
		Providers: []identity.LegacyProvider{
			{
				ID:      "chatgpt:ninhle21199",
				Type:    "chatgpt_web",
				Account: "ninhle21199@gmail.com",
				Plan:    "free",
			},
			{
				ID:           "codex:ninhle21199",
				Type:         "codex_cli",
				Account:      "ninhle21199@gmail.com",
				Plan:         "pro",
				RefreshToken: "refresh-codex-123",
			},
		},
	}
	docBytes, _ := json.Marshal(doc)
	_ = os.WriteFile(accountsPath, docBytes, 0o600)

	migrated, err := identity.MigrateLegacyAccounts(accountsPath, identitiesPath)
	if err != nil {
		t.Fatalf("MigrateLegacyAccounts failed: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("expected 1 migrated account, got %d", migrated)
	}

	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Identities) != 1 {
		var ids []string
		for _, id := range cfg.Identities {
			ids = append(ids, id.ID)
		}
		t.Fatalf("expected exactly 1 identity (subscription), got %d: %v", len(cfg.Identities), ids)
	}

	winner := cfg.Identities[0]
	if winner.ID != "codex:ninhle21199" {
		t.Errorf("expected subscription 'codex:ninhle21199' to supersede web, got %q", winner.ID)
	}
	if winner.Tier != identity.TierSubscription {
		t.Errorf("expected subscription tier, got %q", winner.Tier)
	}
	if winner.Active {
		t.Errorf("expected subscription account to have Active=false, got true")
	}
	if winner.Metadata == nil || winner.Metadata["disabled"] != true {
		t.Errorf("expected subscription account to have metadata.disabled=true, got %v", winner.Metadata)
	}
}
