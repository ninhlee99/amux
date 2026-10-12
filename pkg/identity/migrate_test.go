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

// ChatGPT Web and Codex are separate products: the same email migrates to one
// identity for each, never merged into the Codex subscription.
func TestMigrateLegacyAccounts_ChatGPTWebAndCodexStaySeparate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)

	accountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	doc := identity.LegacyAccountDoc{
		Providers: []identity.LegacyProvider{
			{ID: "chatgpt:ninhle21199", Type: "chatgpt_web", Account: "ninhle21199@gmail.com", Plan: "free"},
			{ID: "codex:ninhle21199", Type: "codex_cli", Account: "ninhle21199@gmail.com", Plan: "pro", RefreshToken: "refresh-codex-123"},
		},
	}
	docBytes, _ := json.Marshal(doc)
	_ = os.WriteFile(accountsPath, docBytes, 0o600)

	if _, err := identity.MigrateLegacyAccounts(accountsPath, identitiesPath); err != nil {
		t.Fatalf("MigrateLegacyAccounts failed: %v", err)
	}
	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	got := map[string]identity.Identity{}
	for _, id := range cfg.Identities {
		got[id.DisplayProvider()] = id
	}
	if len(cfg.Identities) != 2 || got["ChatGPT Web"].ID != "chatgpt:ninhle21199" || got["Codex"].ID != "codex:ninhle21199" {
		t.Fatalf("expected separate ChatGPT Web + Codex identities, got %+v", cfg.Identities)
	}
	codex := got["Codex"]
	if codex.Tier != identity.TierSubscription || codex.Active {
		t.Errorf("codex identity = %+v, want inactive subscription", codex)
	}
	// Out of the rotation pool by default, but not turned off.
	if codex.CanAutoRotate() {
		t.Errorf("subscription must not join the rotation pool automatically")
	}
	if !identity.IsEnabled(codex) {
		t.Errorf("subscription must not be disabled by migration, got %v", codex.Metadata)
	}
}

// A web session on a paid plan stays a web identity: plan "pro" must not turn
// Gemini Web / Claude Web into an OAuth subscription.
func TestMigrateLegacyAccounts_PaidWebPlanStaysWeb(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)

	accountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	doc := identity.LegacyAccountDoc{
		Providers: []identity.LegacyProvider{
			{ID: "gemini:web:bi117ute", Type: "gemini_web", Account: "bi117.ute@gmail.com", Plan: "pro"},
			{ID: "claude:web:bi117ute", Type: "claude_web", Account: "bi117.ute@gmail.com", Plan: "pro"},
		},
	}
	docBytes, _ := json.Marshal(doc)
	_ = os.WriteFile(accountsPath, docBytes, 0o600)

	if _, err := identity.MigrateLegacyAccounts(accountsPath, identitiesPath); err != nil {
		t.Fatalf("MigrateLegacyAccounts failed: %v", err)
	}
	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Identities) != 2 {
		t.Fatalf("want 2 identities, got %+v", cfg.Identities)
	}
	for _, id := range cfg.Identities {
		if id.Tier != identity.TierWeb || id.AuthType != string(identity.AuthCDP) || !id.Active {
			t.Errorf("%s = tier %s auth %s active %v, want active web/cdp", id.ID, id.Tier, id.AuthType, id.Active)
		}
	}
}

// Identities saved by the old migration as subscriptions are repaired on load.
func TestLoadConfig_RepairsWebStoredAsSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)
	path := filepath.Join(tmpDir, "identities.json")

	cfg := &identity.Config{Identities: []identity.Identity{
		{ID: "gemini:web:bi117ute", Provider: "Gemini Web", Tier: identity.TierSubscription, AuthType: string(identity.AuthOAuth)},
		{ID: "agy:01", Provider: "Antigravity", Tier: identity.TierSubscription, AuthType: string(identity.AuthOAuth)},
	}}
	if err := identity.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := identity.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if g := got.Identities[0]; g.Tier != identity.TierWeb || g.AuthType != string(identity.AuthCDP) || !g.Active {
		t.Errorf("gemini web not repaired: %+v", g)
	}
	if a := got.Identities[1]; a.Tier != identity.TierSubscription || a.Active {
		t.Errorf("antigravity subscription must stay untouched: %+v", a)
	}
}

// A fresh install migrates CLI logins as subscriptions outside the pool, keeps
// accounts the user turned off off, and keeps real API keys as API keys.
func TestMigrateLegacyAccounts_FreshInstallTiers(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMUX_HOME", tmpDir)
	accountsPath := filepath.Join(tmpDir, "accounts.json")
	identitiesPath := filepath.Join(tmpDir, "identities.json")

	off := false
	doc := identity.LegacyAccountDoc{Providers: []identity.LegacyProvider{
		{ID: "claude:code:01", Type: "claude", Account: "me@x.com", Enabled: &off},
		{ID: "codex:01", Type: "codex", Account: "me@x.com"},
		{ID: "agy:01", Type: "antigravity"},
		{ID: "openai:api:01", Type: "openai_compatible", ApiKey: "sk-test"},
	}}
	b, _ := json.Marshal(doc)
	_ = os.WriteFile(accountsPath, b, 0o600)
	if _, err := identity.MigrateLegacyAccounts(accountsPath, identitiesPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := identity.LoadConfig(identitiesPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]identity.Identity{}
	for _, id := range cfg.Identities {
		got[id.ID] = id
	}
	for _, id := range []string{"claude:code:01", "codex:01", "agy:01"} {
		if g := got[id]; !g.IsSubscription() || g.CanAutoRotate() {
			t.Errorf("%s must be a subscription outside the pool, got tier %s pool %v", id, g.Tier, g.CanAutoRotate())
		}
	}
	if identity.IsEnabled(got["claude:code:01"]) {
		t.Error("enabled:false must migrate as turned off")
	}
	if got["openai:api:01"].Tier != identity.TierAPIKey {
		t.Errorf("api key account must stay api_key, got %s", got["openai:api:01"].Tier)
	}
}
