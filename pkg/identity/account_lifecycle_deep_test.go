package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/types"
)

// TestAccountLifecycle_TripleAccountSwitchAndRestart validates the zero-trust workflow:
// Login A -> Login B -> Login C -> Switch A -> Switch B -> Switch C -> Restart -> Switch again
// Result MUST BE: NO LOGIN REQUIRED, zero token corruption, correct active state scoping.
func TestAccountLifecycle_TripleAccountSwitchAndRestart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-triple-account-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idPath := filepath.Join(tmpDir, "identities.json")

	// 1. Login Account A (Claude Code Pro)
	acctA := identity.Identity{
		ID:           "claude:code:01",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 15.0,
		Active:       true,
		Credentials: map[string]string{
			"access_token":  "claude-access-A-111",
			"refresh_token": "claude-refresh-A-111",
		},
		Metadata: map[string]interface{}{
			"email":        "alice@enterprise.com",
			"profile_name": "alice@enterprise.com",
		},
	}
	if err := identity.Upsert(idPath, acctA); err != nil {
		t.Fatalf("login Account A: %v", err)
	}

	// Verify Account A is active
	cfg, err := identity.LoadConfig(idPath)
	if err != nil || len(cfg.Identities) != 1 || !cfg.Identities[0].Active {
		t.Fatalf("Account A verification failed: %v", err)
	}

	// 2. Login Account B (Claude Code Personal)
	acctB := identity.Identity{
		ID:           "claude:code:02",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 0.0,
		Active:       false,
		Credentials: map[string]string{
			"access_token":  "claude-access-B-222",
			"refresh_token": "claude-refresh-B-222",
		},
		Metadata: map[string]interface{}{
			"email":        "bob@personal.io",
			"profile_name": "bob@personal.io",
		},
	}
	if err := identity.Upsert(idPath, acctB); err != nil {
		t.Fatalf("login Account B: %v", err)
	}

	// 3. Login Account C (Claude Code OpenSource)
	acctC := identity.Identity{
		ID:           "claude:code:03",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 5.0,
		Active:       false,
		Credentials: map[string]string{
			"access_token":  "claude-access-C-333",
			"refresh_token": "claude-refresh-C-333",
		},
		Metadata: map[string]interface{}{
			"email":        "charlie@opensource.org",
			"profile_name": "charlie@opensource.org",
		},
	}
	if err := identity.Upsert(idPath, acctC); err != nil {
		t.Fatalf("login Account C: %v", err)
	}

	// 4. Switch to Account A
	if err := identity.SetActive(idPath, "claude:code:01"); err != nil {
		t.Fatalf("switch to A: %v", err)
	}
	assertActiveID(t, idPath, "claude:code:01")

	// 5. Switch to Account B
	if err := identity.SetActive(idPath, "claude:code:02"); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	assertActiveID(t, idPath, "claude:code:02")

	// 6. Switch to Account C
	if err := identity.SetActive(idPath, "claude:code:03"); err != nil {
		t.Fatalf("switch to C: %v", err)
	}
	assertActiveID(t, idPath, "claude:code:03")

	// 7. Restart AMUX simulation: Reload store freshly from disk
	reloadedCfg, err := identity.LoadConfig(idPath)
	if err != nil {
		t.Fatalf("reload after restart: %v", err)
	}
	if len(reloadedCfg.Identities) != 3 {
		t.Fatalf("expected 3 identities after restart, got %d", len(reloadedCfg.Identities))
	}
	assertActiveID(t, idPath, "claude:code:03")

	// 8. Switch again: C -> A
	if err := identity.SetActive(idPath, "claude:code:01"); err != nil {
		t.Fatalf("switch C -> A: %v", err)
	}
	assertActiveID(t, idPath, "claude:code:01")

	// Verify all credentials remain 100% intact and undamaged
	freshA, _ := identity.Get(idPath, "claude:code:01")
	freshB, _ := identity.Get(idPath, "claude:code:02")
	freshC, _ := identity.Get(idPath, "claude:code:03")

	if freshA.Credentials["refresh_token"] != "claude-refresh-A-111" {
		t.Errorf("Account A token corrupted: %v", freshA.Credentials)
	}
	if freshB.Credentials["refresh_token"] != "claude-refresh-B-222" {
		t.Errorf("Account B token corrupted: %v", freshB.Credentials)
	}
	if freshC.Credentials["refresh_token"] != "claude-refresh-C-333" {
		t.Errorf("Account C token corrupted: %v", freshC.Credentials)
	}
}

// TestAccountLifecycle_AccountRecovery tests recovering state when identities.json is removed
// but underlying profile bundles exist.
func TestAccountLifecycle_AccountRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-recovery-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	origBase := os.Getenv("AMUX_DIR")
	_ = os.Setenv("AMUX_DIR", tmpDir)
	defer func() {
		if origBase != "" {
			_ = os.Setenv("AMUX_DIR", origBase)
		} else {
			_ = os.Unsetenv("AMUX_DIR")
		}
	}()

	// Save profile directly into profile store
	entries := []types.ProfileEntry{
		{
			Artifact: types.Artifact{Kind: "file", Path: filepath.Join(tmpDir, "dummy_token.txt")},
			Data:     []byte("access-token-123"),
		},
	}
	if err := profile.SaveDirectProfile("codex", "engineer@corp.com", "engineer@corp.com", entries); err != nil {
		t.Fatalf("save direct profile: %v", err)
	}

	idPath := filepath.Join(tmpDir, "identities.json")
	// Migration discovers the profile bundle and generates the flat identity
	count, err := identity.MigrateLegacyAccounts("", idPath)
	if err != nil {
		t.Fatalf("migration error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migrated account, got %d", count)
	}

	// Verify identity was created and recovered
	recovered, err := identity.Get(idPath, "codex:01")
	if err != nil || recovered == nil {
		t.Fatalf("expected codex:01 to be recovered, got: %v", err)
	}
	if recovered.Email() != "engineer@corp.com" {
		t.Errorf("expected email engineer@corp.com, got %s", recovered.Email())
	}
}

// TestAccountLifecycle_LogoutSecurity ensures removing an account purges credentials
// and updates the active pointer safely.
func TestAccountLifecycle_LogoutSecurity(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-logout-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idPath := filepath.Join(tmpDir, "identities.json")

	id1 := identity.Identity{
		ID:       "claude:code:01",
		Provider: "anthropic",
		Tier:     identity.TierSubscription,
		Active:   true,
		Credentials: map[string]string{
			"access_token": "secret-token-1",
		},
		Metadata: map[string]interface{}{"email": "user1@domain.com"},
	}
	id2 := identity.Identity{
		ID:       "claude:code:02",
		Provider: "anthropic",
		Tier:     identity.TierSubscription,
		Active:   false,
		Credentials: map[string]string{
			"access_token": "secret-token-2",
		},
		Metadata: map[string]interface{}{"email": "user2@domain.com"},
	}

	cfg := &identity.Config{
		ThresholdPct: 90.0,
		Identities:   []identity.Identity{id1, id2},
	}
	if err := identity.SaveConfig(idPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Logout / Remove Account 1
	removed, err := identity.Remove(idPath, "claude:code:01")
	if err != nil || !removed {
		t.Fatalf("failed to remove id1: %v", err)
	}

	// Verify Account 1 is completely erased from store
	found1, _ := identity.Get(idPath, "claude:code:01")
	if found1 != nil {
		t.Errorf("Account 1 should be completely removed, but still exists")
	}

	// Account 2 should still exist unharmed
	found2, err := identity.Get(idPath, "claude:code:02")
	if err != nil || found2 == nil {
		t.Fatalf("Account 2 should still exist: %v", err)
	}
	if found2.Credentials["access_token"] != "secret-token-2" {
		t.Errorf("Account 2 credentials modified unexpectedly")
	}
}

func assertActiveID(t *testing.T, path, expectedID string) {
	t.Helper()
	cfg, err := identity.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	var activeID string
	for _, id := range cfg.Identities {
		if id.Active {
			activeID = id.ID
			break
		}
	}
	if activeID != expectedID {
		t.Fatalf("expected active ID to be %q, got %q", expectedID, activeID)
	}
}
