package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/types"
)

const (
	ClaudeClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeAuthURL     = "https://claude.ai/oauth/authorize"
	ClaudeCallbackPort = 54545
	ClaudeCallbackPath = "/callback"
	ClaudeRedirectURI  = "http://localhost:54545/callback"
	ClaudeScope        = "user:profile user:inference"
)

// LoginClaudeCode executes the standalone OAuth PKCE flow for Claude Code CLI.
func LoginClaudeCode(ctx context.Context, customName string) (*types.Token, string, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, "", fmt.Errorf("generate PKCE: %w", err)
	}
	state := GenerateState()

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		ClaudeAuthURL,
		url.QueryEscape(ClaudeClientID),
		url.QueryEscape(ClaudeRedirectURI),
		url.QueryEscape(ClaudeScope),
		url.QueryEscape(challenge),
		url.QueryEscape(state),
	)

	fmt.Println("Opening browser for Claude Code OAuth login…")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", ClaudeRedirectURI)
	code, err := listenForCallback(ctx, ClaudeCallbackPort, ClaudeCallbackPath, state)
	if err != nil {
		return nil, "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeClaudeCode(code, verifier)
	if err != nil {
		return nil, "", fmt.Errorf("exchange token: %w", err)
	}

	accountEmail := fetchClaudeUserEmail(tokenResp.AccessToken)
	if accountEmail == "" {
		accountEmail = fmt.Sprintf("claude-%d", time.Now().Unix())
	}

	// Prepare credentials payload matching Claude Code format
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli()
	creds := auth.ClaudeCreds{}
	creds.ClaudeAiOauth.AccessToken = tokenResp.AccessToken
	creds.ClaudeAiOauth.RefreshToken = tokenResp.RefreshToken
	creds.ClaudeAiOauth.ExpiresAt = expiresAt
	creds.ClaudeAiOauth.Scopes = []string{"user:profile", "user:inference"}
	creds.ClaudeAiOauth.SubscriptionType = "pro"

	credsBytes, _ := json.Marshal(creds)

	// Save profile in amux
	profileName := customName
	if profileName == "" {
		profileName = accountEmail
	}
	profileName = profile.SanitizeName(profileName)

	spec, _ := profile.LookupToolSpec("claude")
	var art types.Artifact
	if len(spec.Artifacts) > 0 {
		art = spec.Artifacts[0]
	} else {
		art = types.Artifact{Kind: "keychain", Service: auth.ClaudeKeychainService, Account: accountEmail}
	}
	entries := []types.ProfileEntry{{Artifact: art, Data: credsBytes}}
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

func exchangeClaudeCode(code, verifier string) (*auth.OAuthRefreshResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     ClaudeClientID,
		"code":          code,
		"redirect_uri":  ClaudeRedirectURI,
		"code_verifier": verifier,
	})

	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, endpoint := range auth.ClaudeOAuthTokenURLs {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s status %d", endpoint, resp.StatusCode)
			continue
		}
		var out auth.OAuthRefreshResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			lastErr = err
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

func fetchClaudeUserEmail(accessToken string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/users/me", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var user struct {
		Email string `json:"email"`
	}
	if json.NewDecoder(resp.Body).Decode(&user) == nil && user.Email != "" {
		return user.Email
	}
	return ""
}
