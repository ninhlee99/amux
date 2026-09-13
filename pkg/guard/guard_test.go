package guard

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestSanitizer(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("X-Provider", "claude:pro:01")
	req.Header.Set("x-model", "claude-3-5-sonnet")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("Via", "1.1 amux")
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	req.Header.Set("Authorization", "Bearer secret")

	SanitizeOutboundRequest(req)

	if req.Header.Get("X-Provider") != "" {
		t.Errorf("expected X-Provider to be removed")
	}
	if req.Header.Get("x-model") != "" {
		t.Errorf("expected x-model to be removed")
	}
	if req.Header.Get("X-Forwarded-For") != "" {
		t.Errorf("expected X-Forwarded-For to be removed")
	}
	if req.Header.Get("Via") != "" {
		t.Errorf("expected Via to be removed")
	}
	if req.Header.Get("User-Agent") != "claude-cli/1.0.0" {
		t.Errorf("expected User-Agent preserved, got %s", req.Header.Get("User-Agent"))
	}
	if req.Header.Get("Authorization") != "Bearer secret" {
		t.Errorf("expected Authorization preserved")
	}
}

func TestPacer_SpacingAndBackoff(t *testing.T) {
	cfg := PacerConfig{
		MinRequestInterval: 10 * time.Millisecond,
		InitialBackoff:     20 * time.Millisecond,
		MaxBackoff:         100 * time.Millisecond,
	}
	p := NewPacer(cfg)

	// Normal pacing
	ctx := context.Background()
	start := time.Now()
	if err := p.Pace(ctx, "acc1", false); err != nil {
		t.Fatalf("unexpected pace err: %v", err)
	}
	if err := p.Pace(ctx, "acc1", false); err != nil {
		t.Fatalf("unexpected pace err: %v", err)
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Errorf("expected pacing delay of at least 10ms")
	}

	// Backoff
	p.RecordRateLimit("acc1", 30*time.Millisecond)
	inBo, remaining := p.InBackoff("acc1")
	if !inBo || remaining <= 0 {
		t.Fatalf("expected in backoff")
	}
	if err := p.Pace(ctx, "acc1", false); !errors.Is(err, ErrAccountInBackoff) {
		t.Errorf("expected ErrAccountInBackoff, got %v", err)
	}

	// Clearing backoff
	p.ClearBackoff("acc1")
	inBo, _ = p.InBackoff("acc1")
	if inBo {
		t.Errorf("expected backoff cleared")
	}
}

func TestHealthTracker_ScoringAndQuarantine(t *testing.T) {
	ht := NewHealthTracker()
	acc := "claude:01"

	// Initially healthy
	rep := ht.GetReport(acc)
	if rep.Score != 100 || rep.Status != StatusHealthy {
		t.Fatalf("expected initial score 100, got %d (status: %s)", rep.Score, rep.Status)
	}

	// 1 rate limit -> score drops
	ht.RecordRateLimit(acc, 0)
	rep = ht.GetReport(acc)
	if rep.Score != 80 || rep.Status != StatusHealthy {
		t.Errorf("expected score 80 after 1 rate limit, got %d", rep.Score)
	}

	// 2 auth errors -> rapid drop & quarantine
	ht.RecordAuthError(acc, "401 unauthorized")
	ht.RecordAuthError(acc, "403 forbidden")

	isQ, dur, reason := ht.IsQuarantined(acc)
	if !isQ {
		t.Fatalf("expected account to be quarantined after consecutive auth errors")
	}
	if dur <= 0 || reason == "" {
		t.Errorf("expected positive quarantine duration and reason, got %v, %s", dur, reason)
	}

	rep = ht.GetReport(acc)
	if rep.Status != StatusQuarantined {
		t.Errorf("expected status quarantined, got %s", rep.Status)
	}

	// Reset clears quarantine
	ht.Reset(acc)
	isQ, _, _ = ht.IsQuarantined(acc)
	if isQ {
		t.Errorf("expected quarantine cleared after reset")
	}
}

func TestSessionAffinity(t *testing.T) {
	sa := NewSessionAffinity(50 * time.Millisecond)

	req := &types.ChatRequest{
		Metadata: map[string]any{
			"session_id": "sess-xyz",
		},
	}
	key := ExtractSessionKey(nil, req)
	if key != "sess-xyz" {
		t.Fatalf("expected extracted key 'sess-xyz', got %q", key)
	}

	sa.Pin(key, "account-A")
	acc, ok := sa.GetPinned(key)
	if !ok || acc != "account-A" {
		t.Fatalf("expected account-A pinned, got %q (ok=%v)", acc, ok)
	}

	// Wait for TTL expiration
	time.Sleep(60 * time.Millisecond)
	acc, ok = sa.GetPinned(key)
	if ok {
		t.Errorf("expected affinity expired, but got %q", acc)
	}
}

func TestProxyEgress(t *testing.T) {
	c1, err := GetClientForProxy("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c1 == nil {
		t.Fatalf("expected non-nil client")
	}

	// Verify caching returns identical client pointer
	c2, err := GetClientForProxy("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c1 != c2 {
		t.Errorf("expected cached client pointer to match")
	}

	// Empty proxy should return nil
	emptyClient, err := GetClientForProxy("")
	if err != nil || emptyClient != nil {
		t.Errorf("expected nil client for empty proxy url")
	}
}
