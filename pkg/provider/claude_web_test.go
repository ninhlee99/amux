package provider

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestClaudeFreeRateLimitErr_CarriesResetTime(t *testing.T) {
	reset := time.Now().Add(90 * time.Minute).Unix()
	body := []byte(fmt.Sprintf(`{"type":"error","error":{"type":"rate_limit_error","message":"{\"type\":\"exceeded_limit\",\"resetsAt\":%d,\"remaining\":null}"}}`, reset))
	err := claudeFreeRateLimitErr("claude:web:x", body, 0)
	if !errors.Is(err, types.ErrRateLimitReached) {
		t.Fatalf("must stay a rate-limit error: %v", err)
	}
	if d := types.ExtractRetryAfter(err); d < 85*time.Minute || d > 91*time.Minute {
		t.Fatalf("retry-after = %s, want ~90m", d)
	}
	if d := types.ExtractRetryAfter(claudeFreeRateLimitErr("x", nil, 42)); d != 42*time.Second {
		t.Fatalf("Retry-After header must win, got %s", d)
	}
}

func TestChatGPTJWTExpired(t *testing.T) {
	mk := func(exp int64) string {
		p := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
		return "h." + p + ".s"
	}
	now := time.Now()
	if !chatgptJWTExpired(mk(now.Add(-time.Hour).Unix()), now) {
		t.Error("past exp must be expired")
	}
	if chatgptJWTExpired(mk(now.Add(time.Hour).Unix()), now) {
		t.Error("future exp must be valid")
	}
	if chatgptJWTExpired("opaque-token", now) {
		t.Error("unreadable token must not count as expired")
	}
	if !isChatGPTAuthFailure(errors.New(`sentinel status 401: {"code":"token_expired"}`)) {
		t.Error("sentinel 401 is an auth failure")
	}
}
