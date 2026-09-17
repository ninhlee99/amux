package gateway_test

import (
	"os"
	"testing"

	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/identity"
)

func TestGateway_HookClaudeLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-claude-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	// Initially not hooked
	hooked, _ := gateway.IsClaudeHooked()
	if hooked {
		t.Fatalf("expected Claude not hooked initially")
	}

	// Hook Claude
	testURL := "http://127.0.0.1:8787"
	if err := gateway.HookClaude(testURL); err != nil {
		t.Fatalf("HookClaude error: %v", err)
	}

	hooked, val := gateway.IsClaudeHooked()
	if !hooked || val != testURL {
		t.Fatalf("expected hooked with %s, got hooked=%v, val=%s", testURL, hooked, val)
	}

	// Unhook Claude
	if err := gateway.UnhookClaude(); err != nil {
		t.Fatalf("UnhookClaude error: %v", err)
	}

	hooked, _ = gateway.IsClaudeHooked()
	if hooked {
		t.Fatalf("expected Claude unhooked")
	}
}

func TestGateway_ConditionalHookAndAutoDetach(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "amux-cond-hook-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	_ = os.Setenv("HOME", tmpDir)

	// 1. When accounts are below threshold -> Should NOT hook
	ids := []identity.Identity{
		{
			ID:           "sub-1",
			Provider:     "anthropic",
			Tier:         identity.TierSubscription,
			UsagePercent: 80.0,
			Active:       true,
		},
	}

	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ := gateway.IsClaudeHooked()
	if hooked {
		t.Errorf("should not hook when subscription has remaining quota")
	}

	// 2. When subscription hits 100% (single account pool rule) -> Injects hook!
	ids[0].UsagePercent = 100.0
	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ = gateway.IsClaudeHooked()
	if !hooked {
		t.Errorf("should hook when all subscriptions are exhausted")
	}

	// 3. When quota resets to 10% -> Auto-detach!
	ids[0].UsagePercent = 10.0
	_ = gateway.CheckAndConditionalHook(ids, 95.0)
	hooked, _ = gateway.IsClaudeHooked()
	if hooked {
		t.Errorf("should auto-detach when subscription resets")
	}
}
