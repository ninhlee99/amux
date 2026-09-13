package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
)

// DeviceFlowConfig holds configuration for RFC 8628 OAuth 2.0 Device Authorization Grant.
// Note: Designed for OAuth 2.0 Public Clients (CLI tools) where client_secret is omitted.
type DeviceFlowConfig struct {
	ProviderLabel string
	ClientID      string
	DeviceAuthURL string
	TokenURL      string
	BaseURL       string
	DefaultModel  string
	IDPrefix      string
	Priority      int
}

// TerminalOAuthError represents a fatal OAuth error (e.g. access_denied, expired_token)
// that should terminate the polling loop immediately.
type TerminalOAuthError struct {
	Code    string
	Message string
}

func (e *TerminalOAuthError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (%s)", e.Code, e.Message)
	}
	return e.Code
}

type deviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// RunDeviceFlow executes RFC 8628 Device Authorization Flow with full compliance
// (including slow_down dynamic backoff, capped retry, and refresh token preservation).
func RunDeviceFlow(ctx context.Context, cfg DeviceFlowConfig, customName string) (string, error) {
	data := url.Values{}
	data.Set("client_id", cfg.ClientID)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DeviceAuthURL, strings.NewReader(data.Encode()))
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

	var authRes deviceAuthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&authRes); err != nil {
		return "", fmt.Errorf("decode device auth response: %w", err)
	}

	verifyURL := authRes.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = authRes.VerificationURI
	}

	fmt.Println()
	fmt.Println("=======================================================")
	fmt.Printf("👉 %s Authentication Code: %s\n", cfg.ProviderLabel, authRes.UserCode)
	fmt.Printf("👉 Visit: %s\n", verifyURL)
	fmt.Println("=======================================================")
	fmt.Println()

	_ = OpenBrowser(verifyURL)

	interval := time.Duration(authRes.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	fmt.Println("Waiting for user approval in browser…")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	timeoutDur := time.Duration(authRes.ExpiresIn) * time.Second
	if timeoutDur <= 0 {
		timeoutDur = 15 * time.Minute
	}
	timeout := time.After(timeoutDur)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", fmt.Errorf("device authorization timed out (expired)")
		case <-ticker.C:
			tok, done, slowDown, err := pollDeviceToken(ctx, client, cfg, authRes.DeviceCode)
			if err != nil {
				var termErr *TerminalOAuthError
				if errors.As(err, &termErr) {
					return "", fmt.Errorf("authorization failed: %w", termErr)
				}
				// Transient network, HTTP 5xx, or temporary gateway hiccup: inform user and continue polling
				fmt.Printf("[notice: transient connection hiccup (%v), retrying…]\n", err)
				continue
			}
			if slowDown {
				// RFC 8628 §3.5: MUST increase interval by 5 seconds.
				// We cap the upper bound at 60s as an operational safety limit to avoid
				// runaway interval inflation while still respecting backoff requests.
				interval += 5 * time.Second
				if interval > 60*time.Second {
					interval = 60 * time.Second
				}
				ticker.Reset(interval)
				continue
			}
			if done && tok != nil {
				fmt.Printf("Authorization successful! Saving %s credentials…\n", cfg.ProviderLabel)
				id := customName
				if id == "" {
					if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), cfg.IDPrefix); err == nil {
						id = next
					} else {
						id = cfg.IDPrefix + ":01"
					}
				}

				// Preserve both AccessToken and RefreshToken in ProviderConfig
				err = provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
					ID:           id,
					Type:         "openai_compatible",
					Priority:     cfg.Priority,
					BaseURL:      cfg.BaseURL,
					APIKey:       tok.AccessToken,
					RefreshToken: tok.RefreshToken,
					Model:        cfg.DefaultModel,
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

func pollDeviceToken(ctx context.Context, client *http.Client, cfg DeviceFlowConfig, deviceCode string) (*deviceTokenResponse, bool, bool, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", cfg.ClientID)
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, false, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, false, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, false, true, nil
	}

	if resp.StatusCode >= 500 {
		return nil, false, false, fmt.Errorf("server error %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, false, fmt.Errorf("read response body: %w", err)
	}

	var tok deviceTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, false, false, &TerminalOAuthError{
				Code:    fmt.Sprintf("http_%d", resp.StatusCode),
				Message: fmt.Sprintf("upstream client error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body))),
			}
		}
		return nil, false, false, fmt.Errorf("decode error: %w", err)
	}

	switch tok.Error {
	case "authorization_pending":
		return nil, false, false, nil
	case "slow_down":
		return nil, false, true, nil
	case "server_error", "temporarily_unavailable":
		return nil, false, false, fmt.Errorf("upstream oauth server error: %s", tok.Error)
	case "access_denied":
		return nil, false, false, &TerminalOAuthError{Code: "access_denied", Message: "user denied authorization"}
	case "expired_token":
		return nil, false, false, &TerminalOAuthError{Code: "expired_token", Message: "device code expired, please re-run login"}
	case "":
		if tok.AccessToken != "" {
			return &tok, true, false, nil
		}
		return nil, false, false, fmt.Errorf("unexpected empty token response (no access_token or error field)")
	default:
		return nil, false, false, &TerminalOAuthError{Code: tok.Error, Message: fmt.Sprintf("%s oauth error", cfg.ProviderLabel)}
	}
}
