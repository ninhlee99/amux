package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
)

const (
	AntigravityAuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	AntigravityTokenURL     = "https://oauth2.googleapis.com/token"
	AntigravityCallbackPort = 51121
	AntigravityCallbackPath = "/oauth-callback"
	AntigravityRedirectURI  = "http://localhost:51121/oauth-callback"
	AntigravityScope        = "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/cloud-platform"
)

func getAntigravityClientID() string {
	if env := os.Getenv("ANTIGRAVITY_CLIENT_ID"); env != "" {
		return env
	}
	dec, _ := base64.StdEncoding.DecodeString("MTA3MTAwNjA2MDU5MS10bWhzc2luMmgyMWxjcmUyMzV2dG9sb2poNGc0MDNlcC5hcHBzLmdvb2dsZXVzZXJjb250ZW50LmNvbQ==")
	return string(dec)
}

func getAntigravityClientSecret() string {
	if env := os.Getenv("ANTIGRAVITY_CLIENT_SECRET"); env != "" {
		return env
	}
	dec, _ := base64.StdEncoding.DecodeString("R09DU1BYLUs1OEZXUjQ4NkxkTEoxbUxCOHNYQzR6cURBZg==")
	return string(dec)
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// LoginAntigravity executes standalone Google OAuth PKCE flow for Antigravity / AGY.
func LoginAntigravity(ctx context.Context, customName string) (string, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return "", fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := GenerateState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	clientID := getAntigravityClientID()
	clientSecret := getAntigravityClientSecret()

	vals := url.Values{}
	vals.Set("response_type", "code")
	vals.Set("client_id", clientID)
	vals.Set("redirect_uri", AntigravityRedirectURI)
	vals.Set("scope", AntigravityScope)
	vals.Set("code_challenge", challenge)
	vals.Set("code_challenge_method", "S256")
	vals.Set("state", state)
	vals.Set("access_type", "offline")
	vals.Set("prompt", "select_account consent")
	authURL := AntigravityAuthURL + "?" + vals.Encode()

	fmt.Println("Opening browser for Google Antigravity OAuth login…")
	fmt.Println("👉 If not signed in: sign in to your Google account.")
	fmt.Println("👉 If already signed in: select an account or choose 'Use another account' to relogin.")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", AntigravityRedirectURI)
	code, err := listenForCallback(ctx, AntigravityCallbackPort, AntigravityCallbackPath, state)
	if err != nil {
		return "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeGoogleCode(ctx, code, verifier)
	if err != nil {
		return "", fmt.Errorf("exchange token: %w", err)
	}

	email := ParseJWTEmail(tokenResp.IDToken)
	if email == "" {
		email = fmt.Sprintf("agy-%d", time.Now().Unix())
	}

	// 1. Save ~/.gemini/antigravity-cli/credentials.json
	home, _ := os.UserHomeDir()
	agyDir := filepath.Join(home, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(agyDir, 0700)
	credsPath := filepath.Join(agyDir, "credentials.json")
	credsPayload, _ := json.MarshalIndent(map[string]any{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"id_token":      tokenResp.IDToken,
		"client_id":     clientID,
		"client_secret": clientSecret,
		"email":         email,
		"expires_at":    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli(),
	}, "", "  ")
	_ = os.WriteFile(credsPath, credsPayload, 0600)

	// Also install to Keychain service "antigravity-service" if available
	_ = auth.KCSet("antigravity-service", email, string(credsPayload))

	// Save profile in amux
	pName := customName
	if pName == "" {
		if existing := profile.ProfileNameForAccount("antigravity", email); existing != "" {
			pName = existing
			fmt.Printf("Account %s already exists — updating profile %q…\n", email, pName)
		} else {
			pName = email
			fmt.Printf("New account %s detected — creating profile %q…\n", email, pName)
		}
	}
	pName = profile.SanitizeName(pName)
	spec, _ := profile.LookupToolSpec("antigravity")
	var art types.Artifact
	if len(spec.Artifacts) > 0 {
		art = spec.Artifacts[0]
	} else {
		art = types.Artifact{Kind: "keychain", Service: "gemini", Account: "antigravity"}
	}
	entries := []types.ProfileEntry{{Artifact: art, Data: credsPayload}}
	_ = profile.SaveDirectProfile("antigravity", pName, email, entries)

	proxy.Sync()
	return email, nil
}

func exchangeGoogleCode(ctx context.Context, code, verifier string) (*googleTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", getAntigravityClientID())
	data.Set("client_secret", getAntigravityClientSecret())
	data.Set("code", code)
	data.Set("redirect_uri", AntigravityRedirectURI)
	data.Set("code_verifier", verifier)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AntigravityTokenURL, strings.NewReader(data.Encode()))
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
		return nil, fmt.Errorf("google token endpoint returned status %d", resp.StatusCode)
	}

	var tr googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &tr, nil
}
