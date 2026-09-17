package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
)

const (
	CodexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexAuthURL      = "https://auth.openai.com/oauth/authorize"
	CodexCallbackPort = 1455
	CodexCallbackPath = "/auth/callback"
	CodexRedirectURI  = "http://localhost:1455/auth/callback"
	CodexScope        = "openid profile email offline_access api.connectors.read api.connectors.invoke"
)

var CodexTokenURL = "https://auth.openai.com/oauth/token"

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// LoginCodex executes standalone OAuth PKCE flow for OpenAI Codex.
func LoginCodex(ctx context.Context, customName string) (string, error) {
	// Snapshot currently active login on the system before starting new login
	_, _ = syncExistingCodexAuth("")

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
	vals.Set("id_token_add_organizations", "true")
	vals.Set("codex_cli_simplified_flow", "true")
	vals.Set("prompt", "login")
	vals.Set("state", state)
	vals.Set("originator", "codex_cli_rs")
	authURL := CodexAuthURL + "?" + vals.Encode()

	fmt.Println("Opening browser for OpenAI Codex OAuth login…")
	fmt.Println("👉 If not signed in: sign in to your OpenAI account.")
	fmt.Println("👉 If already signed in: enter credentials or relogin with the intended account.")
	fmt.Printf("If browser does not open automatically, visit:\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	fmt.Printf("Waiting for browser callback on %s…\n", CodexRedirectURI)
	fmt.Println("👉 (Remote/Headless/SSH) If browser does not redirect to localhost, paste the authorization code or full redirect URL here:")
	code, err := listenForCallback(ctx, CodexCallbackPort, CodexCallbackPath, state)
	if err != nil {
		return "", fmt.Errorf("waiting for OAuth callback: %w", err)
	}

	fmt.Println("Authorization code received. Exchanging for tokens…")
	tokenResp, err := exchangeCodexCode(ctx, code, verifier)
	if err != nil {
		return "", fmt.Errorf("exchange token: %w", err)
	}

	email, accountID, plan := ParseCodexClaims(tokenResp.IDToken)
	if email == "" {
		email, _, plan = ParseCodexClaims(tokenResp.AccessToken)
	}
	if email == "" {
		email = ParseJWTEmail(tokenResp.IDToken)
	}
	if email == "" {
		email = ParseJWTEmail(tokenResp.AccessToken)
	}
	if email == "" {
		email = fmt.Sprintf("codex-%d", time.Now().Unix())
	}

	if plan == "pro" {
		fmt.Printf("✨ OpenAI Subscription detected (Plus/Pro/Team) for %s -> Configured as codex CLI.\n", email)
	} else {
		fmt.Printf("ℹ️ Free OpenAI account detected for %s -> Configured in codex free pool.\n", email)
	}

	// 1. Save ~/.codex/auth.json in the structure expected by Codex CLI
	home, _ := os.UserHomeDir()
	codexDir := filepath.Join(home, ".codex")
	_ = os.MkdirAll(codexDir, 0700)
	authJSONPath := filepath.Join(codexDir, "auth.json")

	tokensMap := map[string]any{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"id_token":      tokenResp.IDToken,
	}
	if accountID != "" {
		tokensMap["account_id"] = accountID
	}

	doc := map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens":         tokensMap,
		"last_refresh":   time.Now().UTC().Format(time.RFC3339),
	}
	authPayload, _ := json.MarshalIndent(doc, "", "  ")
	_ = os.WriteFile(authJSONPath, authPayload, 0600)

	// 2. Save profile in amux profile manager
	profileName := customName
	if profileName == "" {
		if existing := profile.ProfileNameForAccount("codex", email); existing != "" {
			profileName = existing
			fmt.Printf("Account %s already exists — updating profile %q…\n", email, profileName)
		} else {
			profileName = email
			fmt.Printf("New account %s detected — creating profile %q…\n", email, profileName)
		}
	}
	profileName = profile.SanitizeName(profileName)

	entries := []types.ProfileEntry{
		{
			Artifact: types.Artifact{Kind: "file", Path: authJSONPath, AccountField: "jwt:tokens.id_token:email"},
			Data:     authPayload,
		},
	}
	if err := profile.SaveDirectProfile("codex", profileName, email, entries); err != nil {
		fmt.Printf("Warning: saving profile bundle: %v\n", err)
	}

	// 3. Add or update in amux accounts.json
	slot := provider.ResolvePoolSlot(provider.DefaultAccountsPath(), "codex_cli", email)
	id := customName
	if id == "" {
		id = slot.ID
		if slot.Relogin {
			if slot.RenameFrom != "" {
				fmt.Printf("Account %s already exists — upgrading provider %s → %s…\n", email, slot.RenameFrom, id)
			} else {
				fmt.Printf("Account %s already exists — updating provider %s…\n", email, id)
			}
		} else {
			fmt.Printf("New account %s detected — creating provider %s…\n", email, id)
		}
	}
	err = provider.UpsertPoolProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:           id,
		Type:         "codex_cli",
		Priority:     5,
		Account:      email,
		Plan:         plan,
		Model:        "gpt-4o",
		RefreshToken: tokenResp.RefreshToken,
	}, slot.RenameFrom)
	if err != nil {
		return "", fmt.Errorf("save provider: %w", err)
	}

	proxy.Sync()
	return email, nil
}

// LoginCodexDeviceFlow executes the native device code flow using codex CLI if available,
// or falls back to web OAuth flow with dual-channel terminal prompt.
func LoginCodexDeviceFlow(ctx context.Context, customName string) (string, error) {
	// Snapshot currently active login on the system before starting new login
	_, _ = syncExistingCodexAuth("")

	codexPath := findCodexBinary()
	if codexPath != "" {
		fmt.Printf("Found Codex CLI at %s\n", codexPath)
		fmt.Println("Initiating ChatGPT device authorization via Codex CLI…")
		cmd := exec.CommandContext(ctx, codexPath, "login", "--device-auth")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("codex login --device-auth failed: %w", err)
		}

		return syncExistingCodexAuth(customName)
	}

	fmt.Println("Codex CLI binary not found locally. Falling back to standalone OAuth flow…")
	return LoginCodex(ctx, customName)
}

func findCodexBinary() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "codex"),
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

func syncExistingCodexAuth(customName string) (string, error) {
	home, _ := os.UserHomeDir()
	authJSONPath := filepath.Join(home, ".codex", "auth.json")
	data, err := os.ReadFile(authJSONPath)
	if err != nil {
		return "", fmt.Errorf("read ~/.codex/auth.json: %w", err)
	}

	var doc struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IDToken      string `json:"id_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse ~/.codex/auth.json: %w", err)
	}

	email, _, plan := ParseCodexClaims(doc.Tokens.IDToken)
	if email == "" {
		email, _, plan = ParseCodexClaims(doc.Tokens.AccessToken)
	}
	if email == "" {
		email = ParseJWTEmail(doc.Tokens.IDToken)
	}
	if email == "" {
		email = ParseJWTEmail(doc.Tokens.AccessToken)
	}
	if email == "" {
		email = fmt.Sprintf("codex-%d", time.Now().Unix())
	}

	// Save profile in amux
	profileName := customName
	if profileName == "" {
		if existing := profile.ProfileNameForAccount("codex", email); existing != "" {
			profileName = existing
			fmt.Printf("Account %s already exists — updating profile %q…\n", email, profileName)
		} else {
			profileName = email
			fmt.Printf("New account %s detected — creating profile %q…\n", email, profileName)
		}
	}
	profileName = profile.SanitizeName(profileName)

	entries := []types.ProfileEntry{
		{
			Artifact: types.Artifact{Kind: "file", Path: authJSONPath, AccountField: "jwt:tokens.id_token:email"},
			Data:     data,
		},
	}
	_ = profile.SaveDirectProfile("codex", profileName, email, entries)

	// Add or update in amux accounts.json
	slot := provider.ResolvePoolSlot(provider.DefaultAccountsPath(), "codex_cli", email)
	id := customName
	if id == "" {
		id = slot.ID
	}
	err = provider.UpsertPoolProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:           id,
		Type:         "codex_cli",
		Priority:     5,
		Account:      email,
		Plan:         plan,
		Model:        "gpt-4o",
		RefreshToken: doc.Tokens.RefreshToken,
	}, slot.RenameFrom)
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

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read codex token response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s status %d: %s", CodexTokenURL, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out codexTokenResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode codex token: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("empty access token received from %s", CodexTokenURL)
	}
	return &out, nil
}
