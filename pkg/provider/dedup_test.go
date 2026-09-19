package provider

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
)

func makeFakeJWT(email string, iat int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadStr := fmt.Sprintf(`{"email":"%s","iat":%d,"exp":%d,"https://api.openai.com/profile":{"email":"%s"}}`, email, iat, iat+86400, email)
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadStr))
	return header + "." + payload + ".sig"
}

func TestDeduplicateProviders_KeepsLatestLogin(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "accounts.json")

	// pOld has older iat and numeric ID chatgpt:01
	pOld := ProviderConfig{
		ID:           "chatgpt:01",
		Type:         "chatgpt_web",
		Priority:     1,
		SessionToken: makeFakeJWT("user@example.com", 1000),
	}
	// pNew has newer iat and named ID chatgpt:user
	pNew := ProviderConfig{
		ID:           "chatgpt:user",
		Type:         "chatgpt_web",
		Account:      "user@example.com",
		Priority:     1,
		SessionToken: makeFakeJWT("user@example.com", 2000),
	}
	// pGhost has empty credentials
	pGhost := ProviderConfig{
		ID:       "codex:01",
		Type:     "codex_cli",
		Priority: 1,
	}
	pValidCodex := ProviderConfig{
		ID:           "codexcli:01",
		Type:         "codex_cli",
		Account:      "coder@example.com",
		Priority:     1,
		RefreshToken: "valid-refresh-token",
	}

	f := &AccountsFile{
		Providers: []ProviderConfig{pOld, pNew, pGhost, pValidCodex},
	}
	if err := SaveConfigFile(cfgPath, f); err != nil {
		t.Fatalf("save: %v", err)
	}

	removed, err := DeduplicateProviders(cfgPath)
	if err != nil {
		t.Fatalf("dedup: %v", err)
	}

	// Should remove chatgpt:01 and codex:01
	removedMap := map[string]bool{}
	for _, r := range removed {
		removedMap[r] = true
	}
	if !removedMap["chatgpt:01"] {
		t.Errorf("expected chatgpt:01 to be removed as older duplicate, got %v", removed)
	}
	if !removedMap["codex:01"] {
		t.Errorf("expected codex:01 to be removed as ghost, got %v", removed)
	}

	file, err := LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(file.Providers) != 2 {
		t.Fatalf("expected 2 providers remaining, got %d: %+v", len(file.Providers), file.Providers)
	}
	// Verify that chatgpt:user was kept with the newer token
	var foundChatGPT, foundCodex bool
	for _, p := range file.Providers {
		if p.ID == "chatgpt:user" {
			foundChatGPT = true
			if p.Account != "user@example.com" {
				t.Errorf("expected Account user@example.com, got %s", p.Account)
			}
		}
		if p.ID == "codexcli:01" {
			foundCodex = true
		}
	}
	if !foundChatGPT {
		t.Errorf("expected chatgpt:user to be kept")
	}
	if !foundCodex {
		t.Errorf("expected codexcli:01 to be kept")
	}
}

func TestResolvePoolSlot_PromoteLegacyNumericOnRelogin(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "accounts.json")

	// Existing provider has legacy numeric ID codexcli:01
	_ = AddOrUpdateProvider(cfgPath, ProviderConfig{
		ID:           "codexcli:01",
		Type:         "codex_cli",
		Priority:     5,
		Account:      "bi117.ute@gmail.com",
		RefreshToken: "tok1",
	})

	slot := ResolvePoolSlot(cfgPath, "codex_cli", "bi117.ute@gmail.com")
	if !slot.Relogin {
		t.Errorf("expected slot.Relogin=true, got false")
	}
	if slot.ID != "codex:bi117ute" {
		t.Errorf("expected promoted slot.ID=codex:bi117ute, got %s", slot.ID)
	}
	if slot.RenameFrom != "codexcli:01" {
		t.Errorf("expected slot.RenameFrom=codexcli:01, got %s", slot.RenameFrom)
	}

	// Upsert with slot.RenameFrom should replace codexcli:01 with codex:bi117ute
	err := UpsertPoolProvider(cfgPath, ProviderConfig{
		ID:           slot.ID,
		Type:         "codex_cli",
		Priority:     slot.Priority,
		Account:      "bi117.ute@gmail.com",
		RefreshToken: "tok2-updated",
	}, slot.RenameFrom)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	file, _ := LoadConfigFile(cfgPath)
	if len(file.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(file.Providers))
	}
	if file.Providers[0].ID != "codex:bi117ute" {
		t.Errorf("expected ID codex:bi117ute, got %s", file.Providers[0].ID)
	}
	if file.Providers[0].RefreshToken != "tok2-updated" {
		t.Errorf("expected updated token tok2-updated, got %s", file.Providers[0].RefreshToken)
	}
}

func TestDeduplicateProviders_SubBeatsWebForSameEmail(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "accounts.json")

	// Same email for Claude: one Web, one Subscription (claude_code) saved directly
	f := &AccountsFile{
		Providers: []ProviderConfig{
			{
				ID:         "claude:web:alice",
				Type:       "claude_web",
				Priority:   3,
				Account:    "alice@example.com",
				SessionKey: "web-cookie-123",
			},
			{
				ID:           "claude:code:alice",
				Type:         "claude_code",
				Priority:     1,
				Account:      "alice@example.com",
				RefreshToken: "sub-refresh-456",
				Plan:         "pro",
			},
		},
	}
	if err := SaveConfigFile(cfgPath, f); err != nil {
		t.Fatalf("save: %v", err)
	}

	removed, err := DeduplicateProviders(cfgPath)
	if err != nil {
		t.Fatalf("DeduplicateProviders error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "claude:web:alice" {
		t.Errorf("expected claude:web:alice to be removed, got: %v", removed)
	}

	file, _ := LoadConfigFile(cfgPath)
	if len(file.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(file.Providers))
	}
	if file.Providers[0].ID != "claude:code:alice" {
		t.Errorf("expected subscription claude:code:alice to win, got %s", file.Providers[0].ID)
	}
}

func TestAddOrUpdateProvider_SubBeatsWebForSameEmail(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "accounts.json")

	// 1. Add Claude Web
	err := AddOrUpdateProvider(cfgPath, ProviderConfig{
		ID:         "claude:web:alice",
		Type:       "claude_web",
		Priority:   3,
		Account:    "alice@example.com",
		SessionKey: "web-cookie-123",
	})
	if err != nil {
		t.Fatalf("add web: %v", err)
	}

	// 2. Add Claude Subscription with same email -> upgrades existing
	err = AddOrUpdateProvider(cfgPath, ProviderConfig{
		ID:           "claude:code:alice",
		Type:         "claude_code",
		Priority:     1,
		Account:      "alice@example.com",
		RefreshToken: "sub-refresh-456",
		Plan:         "pro",
	})
	if err != nil {
		t.Fatalf("add sub: %v", err)
	}

	file, _ := LoadConfigFile(cfgPath)
	if len(file.Providers) != 1 {
		t.Fatalf("expected exactly 1 provider, got %d", len(file.Providers))
	}
	if file.Providers[0].ID != "claude:code:alice" || file.Providers[0].Type != "claude_code" {
		t.Errorf("expected claude:code:alice, got %+v", file.Providers[0])
	}

	// 3. Attempting to add Claude Web with same email cannot downgrade subscription
	err = AddOrUpdateProvider(cfgPath, ProviderConfig{
		ID:         "claude:web:alice2",
		Type:       "claude_web",
		Priority:   3,
		Account:    "alice@example.com",
		SessionKey: "new-web-cookie",
	})
	if err != nil {
		t.Fatalf("add web attempt: %v", err)
	}

	file, _ = LoadConfigFile(cfgPath)
	if len(file.Providers) != 1 {
		t.Fatalf("expected still exactly 1 provider, got %d", len(file.Providers))
	}
	if file.Providers[0].ID != "claude:code:alice" || file.Providers[0].Type != "claude_code" {
		t.Errorf("expected subscription to be preserved, got %+v", file.Providers[0])
	}
}

func TestResolvePoolSlot_SubscriptionUpgradesWeb(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "accounts.json")

	// 1. Existing ChatGPT Web account
	_ = AddOrUpdateProvider(cfgPath, ProviderConfig{
		ID:         "chatgpt:bob",
		Type:       "chatgpt_web",
		Priority:   5,
		Account:    "bob@company.com",
		SessionKey: "sess-bob",
	})

	// 2. Bob logs in via Codex (subscription) with same email
	slot := ResolvePoolSlot(cfgPath, "codex_cli", "bob@company.com")
	if !slot.Relogin {
		t.Errorf("expected Relogin=true, got false")
	}
	if slot.RenameFrom != "chatgpt:bob" {
		t.Errorf("expected RenameFrom=chatgpt:bob, got %q", slot.RenameFrom)
	}
	if slot.ID != "codex:bob" {
		t.Errorf("expected slot.ID=codex:bob, got %q", slot.ID)
	}

	// 3. Upsert replaces web entry with subscription entry
	err := UpsertPoolProvider(cfgPath, ProviderConfig{
		ID:           slot.ID,
		Type:         "codex_cli",
		Priority:     slot.Priority,
		Account:      "bob@company.com",
		RefreshToken: "codex-refresh-token",
		Plan:         "pro",
	}, slot.RenameFrom)
	if err != nil {
		t.Fatalf("UpsertPoolProvider error: %v", err)
	}

	file, _ := LoadConfigFile(cfgPath)
	if len(file.Providers) != 1 {
		t.Fatalf("expected exactly 1 provider after upgrade, got %d", len(file.Providers))
	}
	if file.Providers[0].ID != "codex:bob" || file.Providers[0].Type != "codex_cli" {
		t.Errorf("expected codex:bob (codex_cli), got: %+v", file.Providers[0])
	}

	// 4. If Bob tries to add chatgpt_web again, it points to existing subscription slot (no web duplicate)
	webSlot := ResolvePoolSlot(cfgPath, "chatgpt_web", "bob@company.com")
	if !webSlot.Relogin || webSlot.ID != "codex:bob" {
		t.Errorf("expected web login for same email to reuse subscription slot codex:bob, got: %+v", webSlot)
	}
}

