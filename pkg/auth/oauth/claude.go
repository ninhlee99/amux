package oauth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/types"
)

const (
	ClaudeClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeAuthURL           = "https://claude.ai/oauth/authorize"
	ClaudeCallbackPort      = 54545
	ClaudeCallbackPath      = "/callback"
	ClaudeRedirectURI       = "http://localhost:54545/callback"
	ClaudeManualRedirectURI = "https://platform.claude.com/oauth/code/callback"
	ClaudeScope             = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload user:plugins"
)

type ClaudeUserProfile struct {
	Account struct {
		UUID  string `json:"uuid"`
		Email string `json:"email"`
	} `json:"account"`
	Organization struct {
		UUID             string `json:"uuid"`
		OrganizationType string `json:"organization_type"`
		RateLimitTier    string `json:"rate_limit_tier"`
		BillingType      string `json:"billing_type"`
	} `json:"organization"`
}

// LoginClaudeCode executes the standalone OAuth PKCE flow for Claude Code CLI.
func LoginClaudeCode(ctx context.Context, customName string) (*types.Token, string, error) {
	// Snapshot currently active login on the system before starting new login
	profile.SyncActiveFromSystem("claude")

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, "", fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return nil, "", fmt.Errorf("generate state: %w", err)
	}

	vals := url.Values{}
	vals.Set("code", "true")
	vals.Set("client_id", ClaudeClientID)
	vals.Set("response_type", "code")
	vals.Set("redirect_uri", ClaudeRedirectURI)
	vals.Set("scope", ClaudeScope)
	vals.Set("code_challenge", challenge)
	vals.Set("code_challenge_method", "S256")
	vals.Set("state", state)
	vals.Set("prompt", "login")
	authURL := ClaudeAuthURL + "?" + vals.Encode()

	fmt.Println("Opening browser for Claude Code OAuth login…")
	fmt.Println("👉 If not signed in: sign in to your Claude account.")
	fmt.Println("👉 If already signed in: enter credentials or relogin with the intended account.")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", ClaudeRedirectURI)
	fmt.Println("👉 (Remote/Headless/SSH) If browser does not redirect to localhost, paste the authorization code or full redirect URL here:")
	code, err := listenForCallback(ctx, ClaudeCallbackPort, ClaudeCallbackPath, state)
	if err != nil {
		return nil, "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeClaudeCodeWithRedirect(ctx, code, verifier, state, ClaudeRedirectURI)
	if err != nil {
		return nil, "", fmt.Errorf("exchange token: %w", err)
	}

	return finishClaudeLogin(ctx, tokenResp, customName)
}

// LoginClaudeCodeManual executes the manual / device code flow for Claude Code CLI
// without requiring local browser callback listener.
func LoginClaudeCodeManual(ctx context.Context, customName string) (*types.Token, string, error) {
	// Snapshot currently active login on the system before starting new login
	profile.SyncActiveFromSystem("claude")

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, "", fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return nil, "", fmt.Errorf("generate state: %w", err)
	}

	vals := url.Values{}
	vals.Set("code", "true")
	vals.Set("client_id", ClaudeClientID)
	vals.Set("response_type", "code")
	vals.Set("redirect_uri", ClaudeManualRedirectURI)
	vals.Set("scope", ClaudeScope)
	vals.Set("code_challenge", challenge)
	vals.Set("code_challenge_method", "S256")
	vals.Set("state", state)
	vals.Set("prompt", "login")
	authURL := ClaudeAuthURL + "?" + vals.Encode()

	fmt.Println()
	fmt.Println("==================================================================")
	fmt.Println("👉 Claude Code Device / Remote OAuth Login")
	fmt.Println("1. Open this link in your browser:")
	fmt.Printf("   %s\n\n", authURL)
	fmt.Println("2. Sign in with your Claude account and click Authorize.")
	fmt.Println("3. Copy the authorization code displayed on screen.")
	fmt.Println("==================================================================")
	fmt.Println()
	_ = OpenBrowser(authURL)

	fmt.Print("Paste authorization code here: ")
	scanner := bufio.NewScanner(os.Stdin)
	var rawCode string
	if scanner.Scan() {
		rawCode = strings.TrimSpace(scanner.Text())
	}
	if rawCode == "" {
		return nil, "", fmt.Errorf("no authorization code entered")
	}

	code := rawCode
	if strings.Contains(rawCode, "code=") {
		if u, err := url.Parse(rawCode); err == nil {
			if c := u.Query().Get("code"); c != "" {
				code = c
			}
		}
	} else if strings.Contains(rawCode, "#") {
		parts := strings.SplitN(rawCode, "#", 2)
		code = strings.TrimSpace(parts[0])
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeClaudeCodeWithRedirect(ctx, code, verifier, state, ClaudeManualRedirectURI)
	if err != nil {
		return nil, "", fmt.Errorf("exchange token: %w", err)
	}

	return finishClaudeLogin(ctx, tokenResp, customName)
}

func finishClaudeLogin(ctx context.Context, tokenResp *auth.OAuthRefreshResponse, customName string) (*types.Token, string, error) {
	userProf := fetchClaudeUserProfile(ctx, tokenResp.AccessToken)
	accountEmail := ""
	if userProf != nil && userProf.Account.Email != "" {
		accountEmail = userProf.Account.Email
	}
	if accountEmail == "" {
		accountEmail = fmt.Sprintf("claude-%d", time.Now().Unix())
	}

	// Prepare credentials payload matching Claude Code format
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli()
	creds := auth.ClaudeCreds{}
	creds.ClaudeAiOauth.AccessToken = tokenResp.AccessToken
	creds.ClaudeAiOauth.RefreshToken = tokenResp.RefreshToken
	creds.ClaudeAiOauth.ExpiresAt = expiresAt
	creds.ClaudeAiOauth.Scopes = strings.Split(ClaudeScope, " ")
	subType := "pro"
	if userProf != nil && userProf.Organization.OrganizationType != "" {
		subType = userProf.Organization.OrganizationType
	}
	creds.ClaudeAiOauth.SubscriptionType = subType

	credsBytes, _ := json.Marshal(creds)

	// Save profile in amux
	profileName := customName
	if profileName == "" {
		if existing := profile.ProfileNameForAccount("claude", accountEmail); existing != "" {
			profileName = existing
			fmt.Printf("Account %s already exists — updating profile %q…\n", accountEmail, profileName)
		} else {
			profileName = accountEmail
			fmt.Printf("New account %s detected — creating profile %q…\n", accountEmail, profileName)
		}
	}
	profileName = profile.SanitizeName(profileName)

	// Update ~/.claude.json with oauthAccount information
	home, _ := os.UserHomeDir()
	claudeJSONPath := filepath.Join(home, ".claude.json")
	var claudeDoc map[string]any
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		_ = json.Unmarshal(data, &claudeDoc)
	}
	if claudeDoc == nil {
		claudeDoc = make(map[string]any)
	}
	oauthAcct, _ := claudeDoc["oauthAccount"].(map[string]any)
	if oauthAcct == nil {
		oauthAcct = make(map[string]any)
	}
	oauthAcct["emailAddress"] = accountEmail
	if userProf != nil {
		if userProf.Account.UUID != "" {
			oauthAcct["accountUuid"] = userProf.Account.UUID
		}
		if userProf.Organization.UUID != "" {
			oauthAcct["organizationUuid"] = userProf.Organization.UUID
		}
		if userProf.Organization.BillingType != "" {
			oauthAcct["billingType"] = userProf.Organization.BillingType
		}
		if userProf.Organization.OrganizationType != "" {
			oauthAcct["organizationType"] = userProf.Organization.OrganizationType
		}
	}
	claudeDoc["oauthAccount"] = oauthAcct
	updatedJSON, _ := json.MarshalIndent(claudeDoc, "", "  ")
	_ = os.WriteFile(claudeJSONPath, updatedJSON, 0o600)

	entries := []types.ProfileEntry{
		{
			Artifact: types.Artifact{Kind: "keychain", Service: auth.ClaudeKeychainService, Account: accountEmail},
			Data:     credsBytes,
		},
		{
			Artifact: types.Artifact{Kind: "file", Path: claudeJSONPath, AccountField: "oauthAccount.emailAddress"},
			Data:     updatedJSON,
		},
	}
	if err := profile.SaveDirectProfile("claude", profileName, accountEmail, entries); err != nil {
		fmt.Printf("Warning: saving profile bundle: %v\n", err)
	}

	// Also install to Keychain if Keychain is available on the system
	_ = auth.KCSet(auth.ClaudeKeychainService, accountEmail, string(credsBytes))

	tok := &types.Token{
		Access:    tokenResp.AccessToken,
		Refresh:   tokenResp.RefreshToken,
		ExpiresAt: time.UnixMilli(expiresAt),
		Remaining: -1,
	}

	return tok, accountEmail, nil
}

func exchangeClaudeCode(ctx context.Context, code, verifier, state string) (*auth.OAuthRefreshResponse, error) {
	return exchangeClaudeCodeWithRedirect(ctx, code, verifier, state, ClaudeRedirectURI)
}

func exchangeClaudeCodeWithRedirect(ctx context.Context, code, verifier, state, redirectURI string) (*auth.OAuthRefreshResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     ClaudeClientID,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
		"state":         state,
	})

	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, endpoint := range auth.ClaudeOAuthTokenURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("%s read error: %w", endpoint, readErr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
			continue
		}
		var out auth.OAuthRefreshResponse
		if err := json.Unmarshal(respBody, &out); err != nil {
			lastErr = fmt.Errorf("%s decode error: %w", endpoint, err)
			continue
		}
		if out.AccessToken == "" {
			lastErr = fmt.Errorf("empty access token from %s", endpoint)
			continue
		}
		return &out, nil
	}
	return nil, fmt.Errorf("token exchange failed: %w", lastErr)
}

func fetchClaudeUserProfile(ctx context.Context, accessToken string) *ClaudeUserProfile {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/profile", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var prof ClaudeUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&prof); err != nil {
		return nil
	}
	return &prof
}
