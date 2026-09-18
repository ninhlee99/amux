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

func TestPace_ZeroLatencyForSubscriptions(t *testing.T) {
	ResetAll()
	t.Cleanup(ResetAll)

	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 100; i++ {
		if err := Pace(ctx, "sub-acc", false); err != nil {
			t.Fatalf("unexpected pace err: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Errorf("Subscription Pace must not add delay, took %v", elapsed)
	}
}

func TestPace_WebAndAPIInterval(t *testing.T) {
	ResetAll()
	t.Cleanup(ResetAll)

	// First request on an account should proceed immediately
	ctx := context.Background()
	start := time.Now()
	if err := Pace(ctx, "chatgpt:test", true); err != nil {
		t.Fatalf("first pace should succeed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("first pace should be immediate, took %v", elapsed)
	}

	// Immediate second request with short context timeout should be canceled / wait
	cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := Pace(cancelCtx, "chatgpt:test", true)
	if err == nil {
		t.Fatalf("expected context timeout due to 10-15s spacing, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}

	// Different account ID should not be blocked by the first account's pacing
	otherCtx, otherCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer otherCancel()
	if err := Pace(otherCtx, "gemini:test", true); err != nil {
		t.Fatalf("different account should not be blocked: %v", err)
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

	// Test Claude Code session headers
	httpReq, _ := http.NewRequest("POST", "/v1/messages", nil)
	httpReq.Header.Set("X-Claude-Code-Session-Id", "claude-sess-999")
	keyFromHeader := ExtractSessionKey(httpReq, nil)
	if keyFromHeader != "claude-sess-999" {
		t.Fatalf("expected X-Claude-Code-Session-Id 'claude-sess-999', got %q", keyFromHeader)
	}

	// Test CheckAndPin account switch detection
	sa2 := NewSessionAffinity(time.Hour)
	switched, prev := sa2.CheckAndPin("sess-1", "acct-alpha")
	if switched || prev != "" {
		t.Fatalf("initial pin should not be a switch, got switched=%v prev=%q", switched, prev)
	}

	// Same account should not be a switch
	switched, prev = sa2.CheckAndPin("sess-1", "acct-alpha")
	if switched {
		t.Fatalf("same account should not be a switch")
	}

	// Different account MUST trigger a switch
	switched, prev = sa2.CheckAndPin("sess-1", "acct-beta")
	if !switched || prev != "acct-alpha" {
		t.Fatalf("expected switch from acct-alpha, got switched=%v prev=%q", switched, prev)
	}

	// Subsequent turn on acct-beta should not trigger a switch
	switched, prev = sa2.CheckAndPin("sess-1", "acct-beta")
	if switched {
		t.Fatalf("subsequent turn on acct-beta should not trigger switch")
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

	wsClient, err := GetClientForProxy("   ")
	if err != nil || wsClient != nil {
		t.Errorf("expected nil client for whitespace proxy url")
	}
}

func TestNewProxyTransport_AllowsSlowFirstByte(t *testing.T) {
	transport, err := NewProxyTransport("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("NewProxyTransport: %v", err)
	}
	if transport.ResponseHeaderTimeout < 5*time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want at least 5m", transport.ResponseHeaderTimeout)
	}
}

func TestHealthTracker_TransientErrorsDoNotQuarantine(t *testing.T) {
	ht := NewHealthTracker()
	acc := "gemini:api:01"
	for i := 0; i < 12; i++ {
		ht.RecordError(acc, errors.New("dial tcp: i/o timeout"))
	}
	if q, _, reason := ht.IsQuarantined(acc); q {
		t.Fatalf("timeout must not quarantine API, got %q", reason)
	}
	rep := ht.GetReport(acc)
	if rep.Status == StatusQuarantined {
		t.Fatalf("status=%s score=%d, want degraded not quarantined", rep.Status, rep.Score)
	}
	if rep.Score >= 80 {
		t.Fatalf("expected score drop after timeouts, got %d", rep.Score)
	}
}

func TestHealthTracker_FalseHTTPCodeInMessageIsNotAuth(t *testing.T) {
	ht := NewHealthTracker()
	ht.RecordError("groq:01", errors.New("upstream timeout after 4032ms"))
	ht.RecordError("groq:01", errors.New("waited 401ms then reset"))
	if q, _, reason := ht.IsQuarantined("groq:01"); q {
		t.Fatalf("substring 401/403 must not count as auth: %s", reason)
	}
	rep := ht.GetReport("groq:01")
	if rep.ConsecutiveAuthErr != 0 {
		t.Fatalf("consecutiveAuthErr=%d, want 0", rep.ConsecutiveAuthErr)
	}
}

func TestHealthTracker_IgnoresCancel(t *testing.T) {
	ht := NewHealthTracker()
	ht.RecordError("openai:01", context.Canceled)
	ht.RecordError("openai:01", context.DeadlineExceeded)
	ht.RecordError("openai:01", errors.New("net/http: request canceled"))
	rep := ht.GetReport("openai:01")
	if rep.Score != 100 || rep.ConsecutiveErrors != 0 {
		t.Fatalf("cancel must not hit health, score=%d errs=%d", rep.Score, rep.ConsecutiveErrors)
	}
}

func TestHealthTracker_RealAuthStillQuarantines(t *testing.T) {
	ht := NewHealthTracker()
	ht.RecordError("groq:01", errors.New("403 forbidden"))
	ht.RecordError("groq:01", errors.New("401 unauthorized"))
	if q, _, _ := ht.IsQuarantined("groq:01"); !q {
		t.Fatal("real 401/403 must still quarantine")
	}
}
