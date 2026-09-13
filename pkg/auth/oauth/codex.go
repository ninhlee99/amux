package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
)

const (
	CodexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexAuthURL      = "https://auth.openai.com/oauth/authorize"
	CodexTokenURL     = "https://auth.openai.com/oauth/token"
	CodexCallbackPort = 1455
	CodexCallbackPath = "/auth/callback"
	CodexRedirectURI  = "http://localhost:1455/auth/callback"
	CodexScope        = "openid profile email offline_access model.request"
)

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// LoginCodex executes standalone OAuth PKCE flow for OpenAI Codex.
func LoginCodex(ctx context.Context, customName string) (string, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return "", fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	vals := url.Values{}
	vals.Set("response_type", "code")
	vals.Set("client_id", CodexClientID)
	vals.Set("redirect_uri", CodexRedirectURI)
	vals.Set("scope", CodexScope)
	vals.Set("code_challenge", challenge)
	vals.Set("code_challenge_method", "S256")
	vals.Set("state", state)
	authURL := CodexAuthURL + "?" + vals.Encode()

	fmt.Println("Opening browser for OpenAI Codex OAuth login…")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", CodexRedirectURI)
	code, err := listenForCallback(ctx, CodexCallbackPort, CodexCallbackPath, state)
	if err != nil {
		return "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeCodexCode(ctx, code, verifier)
	if err != nil {
		return "", fmt.Errorf("exchange token: %w", err)
	}

	email := ParseJWTEmail(tokenResp.IDToken)
	if email == "" {
		email = ParseJWTEmail(tokenResp.AccessToken)
	}
	if email == "" {
		email = fmt.Sprintf("codex-%d", time.Now().Unix())
	}

	// 1. Save ~/.codex/auth.json for local CLI compatibility
	home, _ := os.UserHomeDir()
	codexDir := filepath.Join(home, ".codex")
	_ = os.MkdirAll(codexDir, 0700)
	authJSONPath := filepath.Join(codexDir, "auth.json")
	authPayload, _ := json.MarshalIndent(map[string]string{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"id_token":      tokenResp.IDToken,
	}, "", "  ")
	_ = os.WriteFile(authJSONPath, authPayload, 0600)

	// 2. Add or update in amux accounts.json
	id := customName
	if id == "" {
		slot := provider.ResolvePoolSlot(provider.DefaultAccountsPath(), "codex_cli", email)
		id = slot.ID
		if slot.Relogin {
			fmt.Printf("Account %s already exists — updating provider %s…\n", email, id)
		} else {
			fmt.Printf("New account %s detected — creating provider %s…\n", email, id)
		}
	}
	err = provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:           id,
		Type:         "codex_cli",
		Priority:     5,
		Account:      email,
		Model:        "gpt-4o",
		RefreshToken: tokenResp.RefreshToken,
	})
	if err != nil {
		return "", fmt.Errorf("save provider: %w", err)
	}

	proxy.Sync()
	return email, nil
}

func exchangeCodexCode(ctx context.Context, code, verifier string) (*codexTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", CodexClientID)
	data.Set("code", code)
	data.Set("redirect_uri", CodexRedirectURI)
	data.Set("code_verifier", verifier)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}

	var out codexTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("empty access token received")
	}
	return &out, nil
}
