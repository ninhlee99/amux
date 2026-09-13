package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
)

const (
	GrokClientID       = "xai-cli-oauth"
	GrokDeviceAuthURL  = "https://auth.x.ai/oauth/device_authorization"
	GrokTokenURL       = "https://auth.x.ai/oauth/token"
	GrokAPIBaseURL     = "https://api.x.ai/v1"
	GrokDefaultModel   = "grok-2-latest"
)

type grokDeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type grokTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// LoginGrokDeviceFlow executes the RFC 8628 Device Authorization Flow for xAI Grok.
func LoginGrokDeviceFlow(ctx context.Context, customName string) (string, error) {
	data := url.Values{}
	data.Set("client_id", GrokClientID)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokDeviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device auth endpoint returned status %d", resp.StatusCode)
	}

	var authRes grokDeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authRes); err != nil {
		return "", fmt.Errorf("decode device auth response: %w", err)
	}

	verifyURL := authRes.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = authRes.VerificationURI
	}

	fmt.Println()
	fmt.Println("=======================================================")
	fmt.Printf("👉 xAI Grok Authentication Code: %s\n", authRes.UserCode)
	fmt.Printf("👉 Visit: %s\n", verifyURL)
	fmt.Println("=======================================================")
	fmt.Println()

	_ = OpenBrowser(verifyURL)

	interval := authRes.Interval
	if interval < 3 {
		interval = 5
	}

	fmt.Println("Waiting for user approval in browser…")
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	timeout := time.After(time.Duration(authRes.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", fmt.Errorf("device authorization timed out")
		case <-ticker.C:
			tok, done, err := pollGrokToken(ctx, client, authRes.DeviceCode)
			if err != nil {
				return "", err
			}
			if done && tok != nil {
				fmt.Println("Authorization successful! Saving Grok credentials…")
				id := customName
				if id == "" {
					if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), "grok:api"); err == nil {
						id = next
					} else {
						id = "grok:api:01"
					}
				}

				err = provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
					ID:       id,
					Type:     "openai_compatible",
					Priority: provider.PriorityAPIGrok,
					BaseURL:  GrokAPIBaseURL,
					APIKey:   tok.AccessToken,
					Model:    GrokDefaultModel,
				})
				if err != nil {
					return "", fmt.Errorf("save provider: %w", err)
				}
				proxy.Sync()
				return id, nil
			}
		}
	}
}

func pollGrokToken(ctx context.Context, client *http.Client, deviceCode string) (*grokTokenResponse, bool, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", GrokClientID)
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	var tok grokTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, false, err
	}

	if tok.Error == "authorization_pending" {
		return nil, false, nil
	}
	if tok.Error != "" {
		return nil, false, fmt.Errorf("grok oauth error: %s", tok.Error)
	}
	if tok.AccessToken != "" {
		return &tok, true, nil
	}
	return nil, false, nil
}
