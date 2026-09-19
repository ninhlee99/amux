package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
)

func TestAccountLifecycle_LoginSwitchNoReLogin(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-account-lifecycle-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idPath := filepath.Join(tmpDir, "identities.json")

	// 1. Initial State: Account A is logged in and active
	idA := identity.Identity{
		ID:           "claude:code:01",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 10.0,
		Active:       true,
		Credentials: map[string]string{
			"access_token":  "token-A-initial",
			"refresh_token": "refresh-A-valid",
		},
		Metadata: map[string]interface{}{
			"email":        "alice@example.com",
			"profile_name": "alice@example.com",
		},
	}

	cfg := &identity.Config{
		ThresholdPct: 90.0,
		Identities:   []identity.Identity{idA},
	}
	if err := identity.SaveConfig(idPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	// Verify Account A is active
	loaded, err := identity.LoadConfig(idPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.Identities) != 1 || !loaded.Identities[0].Active {
		t.Fatalf("expected Account A to be active")
	}

	// 2. Add Account B (Login B)
	idB := identity.Identity{
		ID:           "claude:code:02",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 0.0,
		Active:       false,
		Credentials: map[string]string{
			"access_token":  "token-B-initial",
			"refresh_token": "refresh-B-valid",
		},
		Metadata: map[string]interface{}{
			"email":        "bob@example.com",
			"profile_name": "bob@example.com",
		},
	}

	if err := identity.Upsert(idPath, idB); err != nil {
		t.Fatalf("add Account B: %v", err)
	}

	// 3. Switch to Account B
	if err := identity.SetActive(idPath, "claude:code:02"); err != nil {
		t.Fatalf("switch to Account B: %v", err)
	}

	cfgB, err := identity.LoadConfig(idPath)
	if err != nil {
		t.Fatalf("load config after switch: %v", err)
	}

	var activeID string
	for _, id := range cfgB.Identities {
		if id.Active {
			activeID = id.ID
		}
	}
	if activeID != "claude:code:02" {
		t.Fatalf("expected Account B to be active, got %s", activeID)
	}

	// 4. Simulate Account B obtaining a rotated token while active
	updatedB, _ := identity.Get(idPath, "claude:code:02")
	updatedB.Credentials["access_token"] = "token-B-rotated"
	if err := identity.Upsert(idPath, *updatedB); err != nil {
		t.Fatalf("update Account B credentials: %v", err)
	}

	// 5. Switch BACK to Account A (Scenario: Switch A -> NO LOGIN REQUIRED)
	if err := identity.SetActive(idPath, "claude:code:01"); err != nil {
		t.Fatalf("switch back to Account A: %v", err)
	}

	cfgA, err := identity.LoadConfig(idPath)
	if err != nil {
		t.Fatalf("load config after switch back: %v", err)
	}

	foundA := false
	foundB := false
	for _, id := range cfgA.Identities {
		if id.ID == "claude:code:01" {
			foundA = true
			if !id.Active {
				t.Errorf("expected Account A to be active after switch back")
			}
			if id.Credentials["refresh_token"] != "refresh-A-valid" {
				t.Errorf("expected Account A refresh token intact, got %q", id.Credentials["refresh_token"])
			}
		}
		if id.ID == "claude:code:02" {
			foundB = true
			if id.Active {
				t.Errorf("expected Account B to NOT be active after switch back")
			}
			if id.Credentials["access_token"] != "token-B-rotated" {
				t.Errorf("expected Account B rotated token preserved, got %q", id.Credentials["access_token"])
			}
		}
	}

	if !foundA || !foundB {
		t.Fatalf("missing accounts after switch: foundA=%v, foundB=%v", foundA, foundB)
	}
}

func TestAccountLifecycle_CodexAndAntigravitySwitching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-account-multitool-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idPath := filepath.Join(tmpDir, "identities.json")

	// Antigravity accounts
	agy1 := identity.Identity{
		ID:           "antigravity:01",
		Provider:     "gemini",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 15.0,
		Active:       true,
		Credentials: map[string]string{
			"keychain_data": `{"client_secret":"sec1"}`,
		},
		Metadata: map[string]interface{}{"email": "user1@gmail.com"},
	}
	agy2 := identity.Identity{
		ID:           "antigravity:02",
		Provider:     "gemini",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 0.0,
		Active:       false,
		Credentials: map[string]string{
			"keychain_data": `{"client_secret":"sec2"}`,
		},
		Metadata: map[string]interface{}{"email": "user2@gmail.com"},
	}

	// Codex accounts
	codex1 := identity.Identity{
		ID:           "codex:01",
		Provider:     "openai",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 20.0,
		Active:       true,
		Credentials: map[string]string{
			"access_token":  "codex-token-1",
			"refresh_token": "codex-refresh-1",
		},
		Metadata: map[string]interface{}{"email": "dev1@openai.com"},
	}
	codex2 := identity.Identity{
		ID:           "codex:02",
		Provider:     "openai",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		UsagePercent: 0.0,
		Active:       false,
		Credentials: map[string]string{
			"access_token":  "codex-token-2",
			"refresh_token": "codex-refresh-2",
		},
		Metadata: map[string]interface{}{"email": "dev2@openai.com"},
	}

	cfg := &identity.Config{
		ThresholdPct: 90.0,
		Identities:   []identity.Identity{agy1, agy2, codex1, codex2},
	}
	if err := identity.SaveConfig(idPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Switch Antigravity to agy2
	if err := identity.SetActive(idPath, "antigravity:02"); err != nil {
		t.Fatalf("switch antigravity: %v", err)
	}

	// Switch Codex to codex2
	if err := identity.SetActive(idPath, "codex:02"); err != nil {
		t.Fatalf("switch codex: %v", err)
	}

	loaded, err := identity.LoadConfig(idPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	for _, id := range loaded.Identities {
		switch id.ID {
		case "antigravity:01":
			if id.Active {
				t.Errorf("antigravity:01 should be inactive")
			}
		case "antigravity:02":
			if !id.Active {
				t.Errorf("antigravity:02 should be active")
			}
		case "codex:01":
			if id.Active {
				t.Errorf("codex:01 should be inactive")
			}
		case "codex:02":
			if !id.Active {
				t.Errorf("codex:02 should be active")
			}
		}
	}

	// Now switch back to agy1 and codex1 — verify independent tool scoping
	if err := identity.SetActive(idPath, "antigravity:01"); err != nil {
		t.Fatalf("switch back antigravity: %v", err)
	}
	loaded2, _ := identity.LoadConfig(idPath)
	for _, id := range loaded2.Identities {
		if id.ID == "antigravity:01" && !id.Active {
			t.Errorf("antigravity:01 should be active")
		}
		if id.ID == "antigravity:02" && id.Active {
			t.Errorf("antigravity:02 should be inactive")
		}
		// Codex state should remain untouched!
		if id.ID == "codex:02" && !id.Active {
			t.Errorf("codex:02 should still be active")
		}
	}
}

func TestAccountLifecycle_ThresholdAutoRotation(t *testing.T) {
	identities := []identity.Identity{
		{
			ID:           "claude:code:01",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 96.0, // Above 90% threshold!
			Active:       true,
		},
		{
			ID:           "claude:code:02",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 10.0, // Healthy under threshold
			Active:       false,
		},
	}

	next, ok := identity.NextSubscription("anthropic", "claude:code:01", identities, 90.0)
	if !ok {
		t.Fatalf("expected NextSubscription to find eligible account under threshold")
	}
	if next.ID != "claude:code:02" {
		t.Fatalf("expected claude:code:02, got %s", next.ID)
	}
}
