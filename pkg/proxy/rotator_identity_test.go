package proxy

import (
	"path/filepath"
	"testing"

	"amux-accounts/pkg/identity"
)

// A Claude Code subscription the user turned off stays off for the rotator,
// even when its profile was freshly snapshotted without an off flag.
func TestIdentityTurnedOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	cfg := &identity.Config{Identities: []identity.Identity{
		{ID: "claude:code:01", Provider: "Claude Code", Tier: identity.TierSubscription,
			Metadata: map[string]any{"email": "me@x.com", "disabled": true}},
		{ID: "claude:web:x", Provider: "Claude Web", Tier: identity.TierWeb,
			Metadata: map[string]any{"email": "web@x.com", "disabled": true}},
	}}
	if err := identity.SaveConfig(filepath.Join(home, "identities.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if !identityTurnedOff("claude", "ME@x.com") {
		t.Error("turned-off Claude Code identity must disable its profile")
	}
	if identityTurnedOff("claude", "web@x.com") {
		t.Error("a web identity must not affect Claude Code profiles")
	}
	if identityTurnedOff("claude", "other@x.com") || identityTurnedOff("claude", "") {
		t.Error("unknown accounts stay usable")
	}
}
