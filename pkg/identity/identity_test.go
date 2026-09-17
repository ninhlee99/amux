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
			UsagePercent: 96.0,
			Active:       true,
		},
		{
			ID:           "sub-2",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
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
