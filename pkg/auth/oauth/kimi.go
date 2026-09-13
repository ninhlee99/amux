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
	KimiClientID          = "17e5f671-d194-4dfb-9706-5516cb48c098"
	KimiDeviceAuthURL     = "https://auth.kimi.com/api/oauth/device_authorization"
	KimiTokenURL          = "https://auth.kimi.com/api/oauth/token"
	KimiAPIBaseURL        = "https://api.moonshot.cn/v1"
	KimiDefaultModel      = "moonshot-v1-128k"
)

type kimiDeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type kimiTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// LoginKimiDeviceFlow executes the RFC 8628 Device Authorization Flow for Kimi (Moonshot AI).
func LoginKimiDeviceFlow(ctx context.Context, customName string) (string, error) {
	data := url.Values{}
	data.Set("client_id", KimiClientID)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, KimiDeviceAuthURL, strings.NewReader(data.Encode()))
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

	var authRes kimiDeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authRes); err != nil {
		return "", fmt.Errorf("decode device auth response: %w", err)
	}

	verifyURL := authRes.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = authRes.VerificationURI
	}

	fmt.Println()
	fmt.Println("=======================================================")
	fmt.Printf("👉 Kimi Authentication Code: %s\n", authRes.UserCode)
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
			tok, done, err := pollKimiToken(ctx, client, authRes.DeviceCode)
			if err != nil {
				return "", err
			}
			if done && tok != nil {
				fmt.Println("Authorization successful! Saving Kimi credentials…")
				id := customName
				if id == "" {
					if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), "kimi:api"); err == nil {
						id = next
					} else {
						id = "kimi:api:01"
					}
				}

				err = provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
					ID:       id,
					Type:     "openai_compatible",
					Priority: provider.PriorityAPIKimi,
					BaseURL:  KimiAPIBaseURL,
					APIKey:   tok.AccessToken,
					Model:    KimiDefaultModel,
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

func pollKimiToken(ctx context.Context, client *http.Client, deviceCode string) (*kimiTokenResponse, bool, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", KimiClientID)
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, KimiTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	var tok kimiTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, false, err
	}

	if tok.Error == "authorization_pending" {
		return nil, false, nil
	}
	if tok.Error != "" {
		return nil, false, fmt.Errorf("kimi oauth error: %s", tok.Error)
	}
	if tok.AccessToken != "" {
		return &tok, true, nil
	}
	return nil, false, nil
}
