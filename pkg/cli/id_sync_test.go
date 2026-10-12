package cli

import (
	"testing"

	"amux-accounts/pkg/identity"
)

func TestHasSubscriptionFor(t *testing.T) {
	cfg := &identity.Config{Identities: []identity.Identity{
		{ID: "claude:code:01", Provider: "Claude Code", Tier: identity.TierSubscription},
		{ID: "chatgpt:x", Provider: "ChatGPT Web", Tier: identity.TierWeb},
	}}
	if !hasSubscriptionFor(cfg, "claude") {
		t.Error("existing Claude Code subscription must be found")
	}
	if hasSubscriptionFor(cfg, "codex") {
		t.Error("a ChatGPT Web account is not a Codex subscription")
	}
}
