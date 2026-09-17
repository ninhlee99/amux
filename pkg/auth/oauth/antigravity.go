package oauth

import (
	"context"
	"encoding/base64"
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
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
)

const (
	AntigravityAuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	AntigravityCallbackPort = 51121
	AntigravityCallbackPath = "/oauth-callback"
	AntigravityRedirectURI  = "http://localhost:51121/oauth-callback"
	AntigravityScope        = "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/cloud-platform"
)

var AntigravityTokenURL = "https://oauth2.googleapis.com/token"

func getAntigravityClientID() string {
	if env := os.Getenv("ANTIGRAVITY_CLIENT_ID"); env != "" {
		return env
	}
	dec, _ := base64.StdEncoding.DecodeString("MTA3MTAwNjA2MDU5MS10bWhzc2luMmgyMWxjcmUyMzV2dG9sb2poNGc0MDNlcC5hcHBzLmdvb2dsZXVzZXJjb250ZW50LmNvbQ==")
	return string(dec)
}

// placeholderAntigravityClientSecret is the non-functional sample value that was
// committed in place of a real Google OAuth client secret. Google rejects it with
// HTTP 401 "invalid_client", so we detect it and fail with an actionable message
// instead of forwarding it to the token endpoint.
const placeholderAntigravityClientSecret = "GOCSPX-sample-oauth-client-secret"

// ErrAntigravityClientSecretMissing is returned when no usable Google OAuth client
// secret is configured for the Antigravity / AGY flow.
var ErrAntigravityClientSecretMissing = fmt.Errorf(
	"no Google OAuth client secret configured for Antigravity.\n"+
		"This build ships a placeholder value, which Google rejects with "+
		"401 invalid_client.\n"+
		"Set a real secret from your Google Cloud OAuth client before logging in:\n"+
		"  export ANTIGRAVITY_CLIENT_SECRET='GOCSPX-...'\n"+
		"Optionally override the client ID too:\n"+
		"  export ANTIGRAVITY_CLIENT_ID='....apps.googleusercontent.com'")

func getAntigravityClientSecret() (string, error) {
	env := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_SECRET"))
	if env == "" || env == placeholderAntigravityClientSecret {
		return "", ErrAntigravityClientSecretMissing
	}
	return env, nil
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func syncExistingAntigravityAuth() {
	profile.SyncActiveFromSystem("antigravity")
	profile.SyncActiveFromSystem("gemini")

	home, _ := os.UserHomeDir()
	credsPath := filepath.Join(home, ".gemini", "antigravity-cli", "credentials.json")
	data, err := os.ReadFile(credsPath)
	if err == nil {
		var creds struct {
			Email        string `json:"email"`
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(data, &creds); err == nil && creds.Email != "" && creds.RefreshToken != "" {
			slot := provider.ResolvePoolSlot(provider.DefaultAccountsPath(), "antigravity", creds.Email)
			_ = provider.UpsertPoolProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
				ID:           slot.ID,
				Type:         "antigravity",
				Priority:     slot.Priority,
				Account:      creds.Email,
				RefreshToken: creds.RefreshToken,
				Model:        "gemini-2.5-pro",
			}, slot.RenameFrom)
		}
	}
}

// LoginAntigravity executes standalone Google OAuth PKCE flow for Antigravity / AGY.
func LoginAntigravity(ctx context.Context, customName string) (string, error) {
	// Snapshot currently active login on the system before starting new login
	syncExistingAntigravityAuth()

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return "", fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := GenerateState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	clientID := getAntigravityClientID()
	// Validate the client secret before opening a browser, so a misconfigured
	// build fails immediately instead of after the user completes consent.
	clientSecret, err := getAntigravityClientSecret()
	if err != nil {
		return "", err
	}

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
	fmt.Println("👉 (Remote/Headless/SSH) If browser does not redirect to localhost, paste the authorization code or full redirect URL here:")
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

	// 2. Install to Keychain service "antigravity-service"
	_ = auth.KCSet("antigravity-service", email, string(credsPayload))
	_ = auth.KCSet("antigravity-service", "antigravity", string(credsPayload))

	// 3. Install to Keychain service "gemini" account "antigravity" (standard Go keyring format used by IDE & CLIs)
	keyringDoc := map[string]any{
		"token": map[string]any{
			"access_token":  tokenResp.AccessToken,
			"token_type":    "Bearer",
			"refresh_token": tokenResp.RefreshToken,
			"expiry":        time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339Nano),
		},
		"auth_method": "consumer",
		"id_token":    tokenResp.IDToken,
	}
	keyringBytes, _ := json.Marshal(keyringDoc)
	keyringVal := "go-keyring-base64:" + base64.StdEncoding.EncodeToString(keyringBytes)
	_ = auth.KCSet("gemini", "antigravity", keyringVal)

	// 4. Update ~/.gemini/google_accounts.json
	googleAcctsPath := filepath.Join(home, ".gemini", "google_accounts.json")
	var gAccts struct {
		Active string   `json:"active"`
		Old    []string `json:"old"`
	}
	if d, err := os.ReadFile(googleAcctsPath); err == nil {
		_ = json.Unmarshal(d, &gAccts)
	}
	gAccts.Active = email
	found := false
	for _, o := range gAccts.Old {
		if o == email {
			found = true
			break
		}
	}
	if !found {
		gAccts.Old = append(gAccts.Old, email)
	}
	gAcctsBytes, _ := json.MarshalIndent(gAccts, "", "  ")
	_ = os.WriteFile(googleAcctsPath, gAcctsBytes, 0600)

	// 5. Save profile in amux profile manager
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

	entries := []types.ProfileEntry{
		{
			Artifact: types.Artifact{Kind: "keychain", Service: "gemini", Account: "antigravity", AccountField: "jwt:id_token:email"},
			Data:     []byte(keyringVal),
		},
		{
			Artifact: types.Artifact{Kind: "file", Path: googleAcctsPath, AccountField: "active"},
			Data:     gAcctsBytes,
		},
		{
			Artifact: types.Artifact{Kind: "file", Path: credsPath},
			Data:     credsPayload,
		},
	}
	if err := profile.SaveDirectProfile("antigravity", pName, email, entries); err != nil {
		fmt.Printf("Warning: saving antigravity profile bundle: %v\n", err)
	}
	_ = profile.SaveDirectProfile("gemini", pName, email, entries)

	// 6. Register/update in amux accounts.json
	slot := provider.ResolvePoolSlot(provider.DefaultAccountsPath(), "antigravity", email)
	provID := customName
	if provID == "" {
		provID = slot.ID
	}
	_ = provider.UpsertPoolProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:           provID,
		Type:         "antigravity",
		Priority:     5,
		Account:      email,
		Plan:         "pro",
		Model:        "gemini-2.5-pro",
		RefreshToken: tokenResp.RefreshToken,
	}, slot.RenameFrom)

	proxy.Sync()
	return email, nil
}

func exchangeGoogleCode(ctx context.Context, code, verifier string) (*googleTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", getAntigravityClientID())
	clientSecret, err := getAntigravityClientSecret()
	if err != nil {
		return nil, err
	}
	data.Set("client_secret", clientSecret)
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

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read google token response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s status %d: %s", AntigravityTokenURL, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tr googleTokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("decode google token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("empty access token received from %s", AntigravityTokenURL)
	}
	return &tr, nil
}
