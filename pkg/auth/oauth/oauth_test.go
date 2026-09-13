package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	s1, err1 := GenerateState()
	if err1 != nil {
		t.Fatalf("GenerateState failed: %v", err1)
	}
	s2, err2 := GenerateState()
	if err2 != nil {
		t.Fatalf("GenerateState failed: %v", err2)
	}
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

func TestCallbackServer_Security(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "missing_state",
			query:      "?code=123",
			wantStatus: http.StatusBadRequest,
			wantErr:    "state mismatch",
		},
		{
			name:       "wrong_state",
			query:      "?code=123&state=attacker-state",
			wantStatus: http.StatusBadRequest,
			wantErr:    "state mismatch",
		},
		{
			name:       "missing_code",
			query:      "?state=valid-state",
			wantStatus: http.StatusBadRequest,
			wantErr:    "missing code",
		},
	}

	port := 59124
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port++
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			serverErr := make(chan error, 1)
			go func() {
				_, err := listenForCallback(ctx, port, "/callback", "valid-state")
				serverErr <- err
			}()

			time.Sleep(50 * time.Millisecond)

			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback%s", port, tc.query))
			if err != nil {
				t.Fatalf("GET failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			err = <-serverErr
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("server error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPollDeviceToken(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantDone     bool
		wantSlowDown bool
		wantErr      bool
		wantTerminal bool
		wantAccess   string
		wantRefresh  string
	}{
		{
			name:         "pending",
			statusCode:   http.StatusOK,
			responseBody: `{"error":"authorization_pending"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      false,
		},
		{
			name:         "slow_down",
			statusCode:   http.StatusOK,
			responseBody: `{"error":"slow_down"}`,
			wantDone:     false,
			wantSlowDown: true,
			wantErr:      false,
		},
		{
			name:         "access_denied",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"access_denied"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: true,
		},
		{
			name:         "expired_token",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"expired_token"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: true,
		},
		{
			name:         "server_500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"internal_server_error"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: false,
		},
		{
			name:         "malformed_html",
			statusCode:   http.StatusOK,
			responseBody: `<html><head><title>Bad Gateway</title></head></html>`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: false,
		},
		{
			name:         "success",
			statusCode:   http.StatusOK,
			responseBody: `{"access_token":"token-abc","refresh_token":"ref-xyz","expires_in":3600}`,
			wantDone:     true,
			wantSlowDown: false,
			wantErr:      false,
			wantAccess:   "token-abc",
			wantRefresh:  "ref-xyz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.statusCode != 0 {
					w.WriteHeader(tc.statusCode)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.responseBody)
			}))
			defer server.Close()

			cfg := DeviceFlowConfig{
				ProviderLabel: "Test",
				ClientID:      "test-client",
				TokenURL:      server.URL,
			}

			tok, done, slowDown, err := pollDeviceToken(ctx, server.Client(), cfg, "dev-code-123")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && tc.wantTerminal {
				var termErr *TerminalOAuthError
				if !errors.As(err, &termErr) {
					t.Errorf("expected TerminalOAuthError, got: %T (%v)", err, err)
				}
			}
			if tc.wantErr && !tc.wantTerminal {
				var termErr *TerminalOAuthError
				if errors.As(err, &termErr) {
					t.Errorf("expected non-terminal error, got TerminalOAuthError: %v", termErr)
				}
			}
			if done != tc.wantDone {
				t.Errorf("done=%v, want %v", done, tc.wantDone)
			}
			if slowDown != tc.wantSlowDown {
				t.Errorf("slowDown=%v, want %v", slowDown, tc.wantSlowDown)
			}
			if tc.wantDone {
				if tok == nil || tok.AccessToken != tc.wantAccess || tok.RefreshToken != tc.wantRefresh {
					t.Errorf("tok=%+v, want access %q refresh %q", tok, tc.wantAccess, tc.wantRefresh)
				}
			}
		})
	}
}
