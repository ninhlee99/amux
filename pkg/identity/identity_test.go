package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
)

func TestIdentity_ThresholdRules(t *testing.T) {
	// Rule 1: Single-account pool can reach 100%
	singlePool := []identity.Identity{
		{
			ID:           "sub-1",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 96.0,
			Active:       true,
		},
	}

	effSingle := identity.GetEffectiveThreshold("anthropic", singlePool, 95.0)
	if effSingle != 100.0 {
		t.Errorf("expected 100.0 for single-account pool, got %f", effSingle)
	}
	if identity.ShouldFailover(singlePool[0], singlePool, 95.0) {
		t.Errorf("single account at 96%% should not failover under 100%% limit")
	}

	// Rule 2: Multi-account pool triggers at threshold_pct (95.0%)
	multiPool := []identity.Identity{
		{
			ID:           "sub-1",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 96.0,
			Active:       true,
		},
		{
			ID:           "sub-2",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 10.0,
			Active:       false,
		},
	}

	effMulti := identity.GetEffectiveThreshold("anthropic", multiPool, 95.0)
	if effMulti != 95.0 {
		t.Errorf("expected 95.0 for multi-account pool, got %f", effMulti)
	}
	if !identity.ShouldFailover(multiPool[0], multiPool, 95.0) {
		t.Errorf("multi-account at 96%% should failover at 95%% threshold")
	}

	// Verify next available subscription is sub-2
	next, ok := identity.NextSubscription("anthropic", "sub-1", multiPool, 95.0)
	if !ok || next.ID != "sub-2" {
		t.Errorf("expected next subscription to be sub-2, got %v (ok=%v)", next, ok)
	}

	// Verify all subscriptions exhausted
	multiPool[1].UsagePercent = 98.0
	if !identity.AllSubscriptionsExhausted("anthropic", multiPool, 95.0) {
		t.Errorf("expected all subscriptions to be exhausted")
	}
}

func TestIdentity_StoreAndMigrate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-identities-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "identities.json")

	// Upsert an identity
	id := identity.Identity{
		ID:           "test-id-1",
		Provider:     "anthropic",
		Tier:         identity.TierSubscription,
		AuthType:     string(identity.AuthOAuth),
		Credentials:  map[string]string{"access_token": "tok-123"},
		UsagePercent: 50.0,
		Active:       true,
	}
	if err := identity.Upsert(cfgPath, id); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	list, err := identity.List(cfgPath)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 identity, got %d (err: %v)", len(list), err)
	}

	got, err := identity.Get(cfgPath, "test-id-1")
	if err != nil || got == nil || got.Credentials["access_token"] != "tok-123" {
		t.Fatalf("unexpected identity fetched: %v", got)
	}

	// Check Health
	health := identity.CheckHealth(*got)
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}

	// Test SetAutoRotate
	if err := identity.SetAutoRotate(cfgPath, "test-id-1", false); err != nil {
		t.Fatalf("SetAutoRotate error: %v", err)
	}
	updated, _ := identity.Get(cfgPath, "test-id-1")
	if updated.CanAutoRotate() {
		t.Errorf("expected CanAutoRotate false after SetAutoRotate(false)")
	}
}

func TestIdentity_AutoRotateExclusion(t *testing.T) {
	noRotate := false
	pool := []identity.Identity{
		{
			ID:           "sub-1",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 96.0,
			Active:       true,
		},
		{
			ID:           "sub-vip-manual",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 10.0,
			Active:       false,
			AutoRotate:   &noRotate, // Excluded from auto-switch!
		},
		{
			ID:           "sub-3-auto",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 20.0,
			Active:       false,
		},
	}

	// When sub-1 exhausts, NextSubscription must SKIP sub-vip-manual and select sub-3-auto!
	next, ok := identity.NextSubscription("anthropic", "sub-1", pool, 95.0)
	if !ok || next.ID != "sub-3-auto" {
		t.Fatalf("expected NextSubscription to skip manual-only account and select sub-3-auto, got %v (ok=%v)", next, ok)
	}

	// If sub-3-auto also exhausts, sub-vip-manual must STILL NOT be auto-selected!
	pool[2].UsagePercent = 99.0
	nextAfter, okAfter := identity.NextSubscription("anthropic", "sub-1", pool, 95.0)
	if okAfter || nextAfter != nil {
		t.Fatalf("expected NextSubscription to return nil/false when only manual-only accounts remain under threshold, got: %v", nextAfter)
	}

	// HasAvailableSubscription must also ignore manual-only accounts for auto-recovery
	avail, hasAvail := identity.HasAvailableSubscription("anthropic", pool, 95.0)
	if hasAvail || avail != nil {
		t.Fatalf("expected HasAvailableSubscription to return false when only manual-only accounts are available, got: %v", avail)
	}
}

func TestIdentity_PerAccountThreshold(t *testing.T) {
	customThresh := 80.0
	pool := []identity.Identity{
		{
			ID:           "sub-custom",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 82.0,
			Active:       true,
			ThresholdPct: &customThresh,
		},
		{
			ID:           "sub-default",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			AutoRotate:   identity.Pooled(),
			UsagePercent: 88.0,
			Active:       false,
		},
	}

	// sub-custom has explicit threshold 80.0%
	thresh1 := identity.GetAccountThreshold(pool[0], pool, 95.0)
	if thresh1 != 80.0 {
		t.Errorf("expected 80.0 for sub-custom, got %f", thresh1)
	}
	if !identity.ShouldFailover(pool[0], pool, 95.0) {
		t.Errorf("sub-custom at 82%% should failover because threshold is 80%%")
	}

	// sub-default has no custom threshold, multi-account pool base is 95.0%
	thresh2 := identity.GetAccountThreshold(pool[1], pool, 95.0)
	if thresh2 != 95.0 {
		t.Errorf("expected 95.0 for sub-default, got %f", thresh2)
	}
	if identity.ShouldFailover(pool[1], pool, 95.0) {
		t.Errorf("sub-default at 88%% should NOT failover under 95%% threshold")
	}

	// Next subscription from sub-custom should pick sub-default
	next, ok := identity.NextSubscription("anthropic", "sub-custom", pool, 95.0)
	if !ok || next.ID != "sub-default" {
		t.Fatalf("expected next subscription to be sub-default, got %v (ok=%v)", next, ok)
	}
}

// One account per product per email: Claude Web and Claude Code are separate
// products, so the same email keeps one row of each; re-adding the same
// product updates its row instead of duplicating it.
func TestIdentity_OneRowPerProductPerEmail(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "identities.json")

	upsert := func(id identity.Identity) {
		t.Helper()
		if err := identity.Upsert(cfgPath, id); err != nil {
			t.Fatalf("upsert %s: %v", id.ID, err)
		}
	}
	upsert(identity.Identity{
		ID: "claude:web:01", Provider: "anthropic", Tier: identity.TierWeb,
		AuthType:    string(identity.AuthSessionCookie),
		Credentials: map[string]string{"account": "user@example.com", "cookie": "cookie123"},
	})
	upsert(identity.Identity{
		ID: "claude:code:01", Provider: "anthropic", Tier: identity.TierSubscription,
		AuthType:    string(identity.AuthOAuth),
		Credentials: map[string]string{"account": "user@example.com", "access_token": "oauth456"},
	})
	list, _ := identity.List(cfgPath)
	if len(list) != 2 {
		t.Fatalf("expected Claude Web + Claude Code rows, got %d: %+v", len(list), list)
	}

	// Same product again (relogin under another ID) → still one Claude Web row.
	upsert(identity.Identity{
		ID: "claude:web:02", Provider: "anthropic", Tier: identity.TierWeb,
		AuthType:    string(identity.AuthSessionCookie),
		Credentials: map[string]string{"account": "user@example.com", "cookie": "cookie789"},
	})
	list, _ = identity.List(cfgPath)
	if len(list) != 2 {
		t.Fatalf("expected still 2 identities, got %d: %+v", len(list), list)
	}
	for _, it := range list {
		if it.DisplayProvider() == "Claude Web" && it.Credentials["cookie"] != "cookie789" {
			t.Errorf("Claude Web row not updated: %+v", it)
		}
	}
}

func TestIdentity_PoolDefaults(t *testing.T) {
	sub := identity.Identity{ID: "codex:01", Provider: "openai", Tier: identity.TierSubscription}
	web := identity.Identity{ID: "chatgpt:web:01", Provider: "openai", Tier: identity.TierWeb}
	if sub.CanAutoRotate() {
		t.Fatal("subscriptions must not be in the pool until added by hand")
	}
	if !web.CanAutoRotate() {
		t.Fatal("web accounts are in the pool by default")
	}
	sub.AutoRotate = identity.Pooled()
	if !sub.CanAutoRotate() {
		t.Fatal("manually pooled subscription must rotate")
	}
	sub.Metadata = map[string]interface{}{"disabled": true}
	if sub.CanAutoRotate() {
		t.Fatal("a disabled account is never in the pool")
	}
}

func TestIdentity_PoolLookups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identities.json")
	cfg := &identity.Config{Identities: []identity.Identity{
		{ID: "claude:code:01", Provider: "anthropic", Tier: identity.TierSubscription, AutoRotate: identity.Pooled(),
			Metadata: map[string]interface{}{"profile_name": "a@x.com", "email": "a@x.com"}},
		{ID: "claude:code:02", Provider: "anthropic", Tier: identity.TierSubscription,
			Metadata: map[string]interface{}{"profile_name": "b@x.com", "email": "b@x.com"}},
		// A web account with the same email must not make the subscription count as pooled.
		{ID: "claude:web:01", Provider: "anthropic", Tier: identity.TierWeb,
			Metadata: map[string]interface{}{"email": "b@x.com"}},
	}}
	if err := identity.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	if !identity.ProfileInPool(path, "claude", "a@x.com", "") {
		t.Fatal("pooled profile not found")
	}
	if identity.ProfileInPool(path, "claude", "b@x.com", "b@x.com") {
		t.Fatal("unpooled subscription reported as pooled")
	}
	if identity.ProfileInPool(path, "claude", "unknown", "u@x.com") {
		t.Fatal("unknown profile must not be pooled")
	}
	in := identity.PoolMemberFilter(path)
	if !in("claude:code:01") || in("claude:code:02") || in("nope:01") {
		t.Fatal("PoolMemberFilter mismatch")
	}
}

func TestMigrate_V2ClearsLegacySubscriptionDisable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identities.json")
	cfg := &identity.Config{Identities: []identity.Identity{
		{ID: "codex:01", Provider: "openai", Tier: identity.TierSubscription,
			Credentials: map[string]string{"refresh_token": "r"},
			Metadata:    map[string]interface{}{"disabled": true, "email": "a@x.com"}},
	}}
	if err := identity.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.MigrateLegacyAccounts(filepath.Join(dir, "none.json"), path); err != nil {
		t.Fatal(err)
	}
	got, _ := identity.LoadConfig(path)
	if got.Version != 2 || !identity.IsEnabled(got.Identities[0]) {
		t.Fatalf("legacy migration-set disable not cleared: v=%d %+v", got.Version, got.Identities[0].Metadata)
	}
	if got.Identities[0].CanAutoRotate() {
		t.Fatal("clearing the flag must not put the subscription in the pool")
	}
	// Running again keeps a real `account off`.
	_ = identity.SetEnabled(path, "codex:01", false)
	_, _ = identity.MigrateLegacyAccounts(filepath.Join(dir, "none.json"), path)
	got, _ = identity.LoadConfig(path)
	if identity.IsEnabled(got.Identities[0]) {
		t.Fatal("a user's off must survive later migrations")
	}
}
