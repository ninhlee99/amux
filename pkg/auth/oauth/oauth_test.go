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

	"amux-accounts/pkg/auth"
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
	if len(s1) < 43 || len(s2) < 43 {
		t.Errorf("state entropy too low: len=%d, want >= 43", len(s1))
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

func TestParseCodexClaims(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))

	claims := map[string]any{
		"email":              "codex@example.com",
		"chatgpt_account_id": "org-test12345",
		"chatgpt_plan_type":  "pro",
	}
	claimsBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)

	token := fmt.Sprintf("%s.%s.", header, payload)
	email, accountID, plan := ParseCodexClaims(token)
	if email != "codex@example.com" {
		t.Errorf("email = %q, want %q", email, "codex@example.com")
	}
	if accountID != "org-test12345" {
		t.Errorf("accountID = %q, want %q", accountID, "org-test12345")
	}
	if plan != "pro" {
		t.Errorf("plan = %q, want %q", plan, "pro")
	}

	// Test nested https://api.openai.com/auth claims
	nestedClaims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"email":              "nested@example.com",
			"chatgpt_account_id": "org-nested999",
			"chatgpt_plan_type":  "free",
		},
	}
	nestedBytes, _ := json.Marshal(nestedClaims)
	nestedPayload := base64.RawURLEncoding.EncodeToString(nestedBytes)
	nestedToken := fmt.Sprintf("%s.%s.", header, nestedPayload)
	nEmail, nAccountID, nPlan := ParseCodexClaims(nestedToken)
	if nEmail != "nested@example.com" {
		t.Errorf("nested email = %q, want %q", nEmail, "nested@example.com")
	}
	if nAccountID != "org-nested999" {
		t.Errorf("nested accountID = %q, want %q", nAccountID, "org-nested999")
	}
	if nPlan != "free" {
		t.Errorf("nested plan = %q, want %q", nPlan, "free")
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
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control header missing no-store: got %q", cc)
	}
	if csd := resp.Header.Get("Clear-Site-Data"); !strings.Contains(csd, "cookies") {
		t.Errorf("Clear-Site-Data header missing cookies: got %q", csd)
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("server returned error: %v", err)
	}
	if gotCode != "sample-auth-code" {
		t.Errorf("gotCode = %q, want sample-auth-code", gotCode)
	}
}

func TestOpenBrowser(t *testing.T) {
	// OpenPrivateBrowser delegates to OpenBrowser
	ok, _, err := OpenPrivateBrowser("http://127.0.0.1:9999/dummy")
	if ok {
		t.Errorf("expected OpenPrivateBrowser to return false for openedIncognito")
	}
	_ = err
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

			// Retry connection until listener is ready
			var resp *http.Response
			var err error
			for i := 0; i < 20; i++ {
				resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback%s", port, tc.query))
				if err == nil {
					break
				}
				time.Sleep(15 * time.Millisecond)
			}
			if err != nil {
				t.Fatalf("GET failed after retries: %v", err)
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
			name:         "server_error_transient",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"server_error"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: false,
		},
		{
			name:         "temporarily_unavailable_transient",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"temporarily_unavailable"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: false,
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
			name:         "http_429_slow_down",
			statusCode:   http.StatusTooManyRequests,
			responseBody: `{"error":"slow_down"}`,
			wantDone:     false,
			wantSlowDown: true,
			wantErr:      false,
		},
		{
			name:         "empty_200_body",
			statusCode:   http.StatusOK,
			responseBody: `{}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: false,
		},
		{
			name:         "http_401_invalid_client",
			statusCode:   http.StatusUnauthorized,
			responseBody: `{"error":"invalid_client"}`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: true,
		},
		{
			name:         "non_json_400_terminal",
			statusCode:   http.StatusBadRequest,
			responseBody: `<html>Bad Request</html>`,
			wantDone:     false,
			wantSlowDown: false,
			wantErr:      true,
			wantTerminal: true,
		},
		{
			name:         "malformed_html_200",
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

func TestClaudeOAuthExchange(t *testing.T) {
	origURLs := auth.ClaudeOAuthTokenURLs
	defer func() { auth.ClaudeOAuthTokenURLs = origURLs }()

	var receivedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		if receivedBody["state"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Invalid request format"}}`))
			return
		}
		if receivedBody["code"] == "bad_code" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid code"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"claude-access-123","refresh_token":"claude-refresh-456","expires_in":3600}`))
	}))
	defer server.Close()

	auth.ClaudeOAuthTokenURLs = []string{server.URL}

	ctx := context.Background()

	// Test 1: Successful exchange with state
	resp, err := exchangeClaudeCode(ctx, "valid_code", "verifier_secret", "state_random_123")
	if err != nil {
		t.Fatalf("expected success, got err: %v", err)
	}
	if resp.AccessToken != "claude-access-123" || resp.RefreshToken != "claude-refresh-456" {
		t.Errorf("unexpected tokens: %+v", resp)
	}
	if receivedBody["state"] != "state_random_123" {
		t.Errorf("expected state to be sent, got: %q", receivedBody["state"])
	}
	if receivedBody["code_verifier"] != "verifier_secret" {
		t.Errorf("expected code_verifier to be sent, got: %q", receivedBody["code_verifier"])
	}
	if receivedBody["grant_type"] != "authorization_code" {
		t.Errorf("expected grant_type authorization_code, got: %q", receivedBody["grant_type"])
	}

	// Test 2: Error exchange captures detailed body
	_, err = exchangeClaudeCode(ctx, "bad_code", "verifier_secret", "state_random_123")
	if err == nil {
		t.Fatal("expected error on bad_code, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") || !strings.Contains(err.Error(), "Invalid code") {
		t.Errorf("expected detailed error message from response body, got: %v", err)
	}
}

func TestClaudeUserProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		if authHdr != "Bearer test-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"account": {
				"uuid": "acc-111-222",
				"email": "claude.user@example.com"
			},
			"organization": {
				"uuid": "org-333-444",
				"organization_type": "claude_pro",
				"rate_limit_tier": "default_claude_ai",
				"billing_type": "stripe_subscription"
			}
		}`))
	}))
	defer server.Close()

	client := server.Client()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer test-access-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var prof ClaudeUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&prof); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if prof.Account.Email != "claude.user@example.com" {
		t.Errorf("expected email claude.user@example.com, got %q", prof.Account.Email)
	}
	if prof.Account.UUID != "acc-111-222" {
		t.Errorf("expected uuid acc-111-222, got %q", prof.Account.UUID)
	}
	if prof.Organization.OrganizationType != "claude_pro" {
		t.Errorf("expected organization_type claude_pro, got %q", prof.Organization.OrganizationType)
	}
}

func TestCodexOAuthExchange(t *testing.T) {
	origURL := CodexTokenURL
	defer func() { CodexTokenURL = origURL }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") == "bad_code" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"Could not validate your token. Please try signing in again.","code":"token_expired"}}`))
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") != CodexClientID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"codex-acc-123","refresh_token":"codex-ref-456","id_token":"codex-id-789","expires_in":3600}`))
	}))
	defer server.Close()

	CodexTokenURL = server.URL
	ctx := context.Background()

	// Test 1: Successful exchange
	res, err := exchangeCodexCode(ctx, "valid_code", "verifier_secret")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if res.AccessToken != "codex-acc-123" || res.RefreshToken != "codex-ref-456" || res.IDToken != "codex-id-789" {
		t.Errorf("unexpected tokens: %+v", res)
	}

	// Test 2: Error exchange captures detailed body
	_, err = exchangeCodexCode(ctx, "bad_code", "verifier_secret")
	if err == nil {
		t.Fatal("expected error on bad_code, got nil")
	}
	if !strings.Contains(err.Error(), "Could not validate your token") || !strings.Contains(err.Error(), "token_expired") {
		t.Errorf("expected detailed error message from response body, got: %v", err)
	}
}

func TestGoogleOAuthExchange(t *testing.T) {
	origURL := AntigravityTokenURL
	defer func() { AntigravityTokenURL = origURL }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") == "bad_code" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant","error_description":"Malformed auth code."}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"ya29.test-access","refresh_token":"1//test-refresh","id_token":"jwt-test-id","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer server.Close()

	AntigravityTokenURL = server.URL
	ctx := context.Background()

	// Test 1: Successful exchange
	res, err := exchangeGoogleCode(ctx, "valid_code", "verifier_secret")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if res.AccessToken != "ya29.test-access" || res.RefreshToken != "1//test-refresh" {
		t.Errorf("unexpected tokens: %+v", res)
	}

	// Test 2: Error exchange captures detailed body
	_, err = exchangeGoogleCode(ctx, "bad_code", "verifier_secret")
	if err == nil {
		t.Fatal("expected error on bad_code, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") || !strings.Contains(err.Error(), "Malformed auth code") {
		t.Errorf("expected detailed error message from response body, got: %v", err)
	}
}


