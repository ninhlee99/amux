package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}
	if len(verifier) < 43 {
		t.Errorf("verifier too short: %d", len(verifier))
	}
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != expectedChallenge {
		t.Errorf("challenge mismatch: got %q, want %q", challenge, expectedChallenge)
	}
}

func TestGenerateState(t *testing.T) {
	s1 := GenerateState()
	s2 := GenerateState()
	if s1 == "" || s2 == "" {
		t.Errorf("empty state generated")
	}
	if s1 == s2 {
		t.Errorf("state is not random: %s == %s", s1, s2)
	}
}

func TestParseJWTEmail(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))

	claims := map[string]any{
		"sub":   "user-12345",
		"email": "test@example.com",
	}
	claimsBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)

	token := fmt.Sprintf("%s.%s.", header, payload)
	got := ParseJWTEmail(token)
	if got != "test@example.com" {
		t.Errorf("ParseJWTEmail = %q, want %q", got, "test@example.com")
	}

	// Fallback to sub if no email
	claimsNoEmail := map[string]any{"sub": "user-only-sub"}
	claimsNoEmailBytes, _ := json.Marshal(claimsNoEmail)
	payloadNoEmail := base64.RawURLEncoding.EncodeToString(claimsNoEmailBytes)
	gotSub := ParseJWTEmail(fmt.Sprintf("%s.%s.", header, payloadNoEmail))
	if gotSub != "user-only-sub" {
		t.Errorf("ParseJWTEmail fallback = %q, want %q", gotSub, "user-only-sub")
	}

	// Invalid token format
	if ParseJWTEmail("invalid-token") != "" {
		t.Errorf("expected empty for invalid token")
	}
}

func TestSupportedOAuthProviders(t *testing.T) {
	providers := SupportedOAuthProviders()
	if len(providers) < 5 {
		t.Errorf("expected at least 5 providers, got %d", len(providers))
	}
	expected := map[string]bool{
		"claude":      true,
		"codex":       true,
		"antigravity": true,
		"kimi":        true,
		"grok":        true,
	}
	for _, p := range providers {
		delete(expected, p)
	}
	if len(expected) > 0 {
		t.Errorf("missing expected providers: %v", expected)
	}
}

func TestRunOAuth_Unknown(t *testing.T) {
	_, err := RunOAuth(context.Background(), "unknown-service", "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestCallbackServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := 59123
	path := "/test-callback"
	state := "my-secret-state"

	serverErr := make(chan error, 1)
	var gotCode string

	go func() {
		code, err := listenForCallback(ctx, port, path, state)
		if err != nil {
			serverErr <- err
			return
		}
		gotCode = code
		serverErr <- nil
	}()

	// Wait for listener to be ready
	time.Sleep(100 * time.Millisecond)

	// Send callback GET
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s?code=sample-auth-code&state=%s", port, path, state))
	if err != nil {
		t.Fatalf("failed to call callback server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("server returned error: %v", err)
	}
	if gotCode != "sample-auth-code" {
		t.Errorf("gotCode = %q, want sample-auth-code", gotCode)
	}
}
