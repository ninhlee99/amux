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

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
)

const (
	AntigravityClientID     = "REDACTED_CLIENT_ID"
	AntigravityClientSecret = "REDACTED_SECRET"
	AntigravityAuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	AntigravityTokenURL     = "https://oauth2.googleapis.com/token"
	AntigravityCallbackPort = 51121
	AntigravityCallbackPath = "/oauth-callback"
	AntigravityRedirectURI  = "http://localhost:51121/oauth-callback"
	AntigravityScope        = "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/cloud-platform"
)

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
	state := GenerateState()

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s&access_type=offline&prompt=consent",
		AntigravityAuthURL,
		url.QueryEscape(AntigravityClientID),
		url.QueryEscape(AntigravityRedirectURI),
		url.QueryEscape(AntigravityScope),
		url.QueryEscape(challenge),
		url.QueryEscape(state),
	)

	fmt.Println("Opening browser for Google Antigravity OAuth login…")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", AntigravityRedirectURI)
	code, err := listenForCallback(ctx, AntigravityCallbackPort, AntigravityCallbackPath, state)
	if err != nil {
		return "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeGoogleCode(code, verifier)
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
		"client_id":     AntigravityClientID,
		"client_secret": AntigravityClientSecret,
		"email":         email,
		"expires_at":    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli(),
	}, "", "  ")
	_ = os.WriteFile(credsPath, credsPayload, 0600)

	// Also install to Keychain service "antigravity-service" if available
	_ = auth.KCSet("antigravity-service", email, string(credsPayload))

	// Save profile in amux
	pName := customName
	if pName == "" {
		pName = email
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

func exchangeGoogleCode(code, verifier string) (*googleTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", AntigravityClientID)
	data.Set("client_secret", AntigravityClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", AntigravityRedirectURI)
	data.Set("code_verifier", verifier)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, AntigravityTokenURL, strings.NewReader(data.Encode()))
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

	var out googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("empty access token received")
	}
	return &out, nil
}
