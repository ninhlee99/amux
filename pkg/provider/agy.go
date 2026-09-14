package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/types"
)

const (
	agyTokenRefreshLead   = 2 * time.Minute
	agyOAuthTokenURL      = "https://oauth2.googleapis.com/token"
	DefaultAGYModel       = "gemini-2.5-pro"
	DefaultAGYFlashModel  = "gemini-3.6-flash"
	agyBaseURL            = "https://generativelanguage.googleapis.com/v1beta/openai"
)

type agyCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Email        string `json:"email"`
	ExpiresAt    int64  `json:"expires_at"` // unix millisecond
}

// AntigravityAdapter connects amux to Google Antigravity / Gemini via OAuth.
type AntigravityAdapter struct {
	AdapterID   string // e.g. "agy:01"
	PriorityLvl int
	TargetModel string
	GroupLabel  string
	PlanTier    string // "subscription" | "free"
	HTTPClient  *http.Client

	mu    sync.Mutex
	creds *agyCredentials
}

func (a *AntigravityAdapter) ID() string    { return a.AdapterID }
func (a *AntigravityAdapter) Priority() int { return a.PriorityLvl }
func (a *AntigravityAdapter) Group() string {
	if a.GroupLabel != "" {
		return a.GroupLabel
	}
	if strings.EqualFold(a.PlanTier, "free") || strings.Contains(strings.ToLower(a.AdapterID), "free") {
		return "agy_free"
	}
	return "agy_sub"
}

// SupportsTools is true: AGY supports full function calling and tool execution.
func (a *AntigravityAdapter) SupportsTools() bool { return true }

func (a *AntigravityAdapter) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

// AGYCredentialsPath returns the path to Antigravity CLI's credentials file.
func AGYCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "antigravity-cli", "credentials.json")
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "credentials.json")
}

// AGYAuthAvailable reports whether Antigravity credentials or Keychain entry exist.
func AGYAuthAvailable() bool {
	c, err := loadAGYCredentials()
	return err == nil && c != nil && (c.AccessToken != "" || c.RefreshToken != "")
}

func loadAGYCredentials() (*agyCredentials, error) {
	// 1. Try file
	path := AGYCredentialsPath()
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		var c agyCredentials
		if err := json.Unmarshal(b, &c); err == nil && (c.AccessToken != "" || c.RefreshToken != "") {
			return &c, nil
		}
	}

	// 2. Try Keychain service "antigravity-service"
	if s, err := auth.KCGet("antigravity-service", "antigravity"); err == nil && s != "" {
		var c agyCredentials
		if err := json.Unmarshal([]byte(s), &c); err == nil && (c.AccessToken != "" || c.RefreshToken != "") {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("no valid antigravity credentials found")
}

func saveAGYCredentials(c *agyCredentials) error {
	path := AGYCredentialsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	_ = os.WriteFile(path, b, 0600)
	if c.Email != "" {
		_ = auth.KCSet("antigravity-service", c.Email, string(b))
	}
	return nil
}

func (a *AntigravityAdapter) ensureAccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.creds == nil {
		c, err := loadAGYCredentials()
		if err != nil {
			return "", err
		}
		a.creds = c
	}

	expiresAt := time.UnixMilli(a.creds.ExpiresAt)
	if a.creds.AccessToken != "" && time.Now().Add(agyTokenRefreshLead).Before(expiresAt) {
		return a.creds.AccessToken, nil
	}

	if a.creds.RefreshToken == "" {
		if a.creds.AccessToken != "" {
			return a.creds.AccessToken, nil
		}
		return "", fmt.Errorf("antigravity: refresh token missing, please re-login via 'am login agy'")
	}

	// Refresh OAuth token
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", a.creds.ClientID)
	data.Set("client_secret", a.creds.ClientSecret)
	data.Set("refresh_token", a.creds.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agyOAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("refresh token failed (status %d): %s", resp.StatusCode, string(b))
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode token refresh response: %w", err)
	}

	a.creds.AccessToken = tr.AccessToken
	if tr.ExpiresIn > 0 {
		a.creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UnixMilli()
	} else {
		a.creds.ExpiresAt = time.Now().Add(3600 * time.Second).UnixMilli()
	}
	_ = saveAGYCredentials(a.creds)

	return a.creds.AccessToken, nil
}

func (a *AntigravityAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	token, err := a.ensureAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", a.AdapterID, types.ErrAuthentication, err)
	}

	model := a.TargetModel
	if model == "" {
		model = DefaultAGYModel
	}

	// Auto-escalate or fallback for AGY
	if req.TargetTier == "flash" && a.PlanTier == "free" {
		model = DefaultAGYFlashModel
	}

	adapter := &OpenAICompatibleAdapter{
		AdapterID:   a.AdapterID,
		PriorityLvl: a.PriorityLvl,
		BaseURL:     agyBaseURL,
		APIKey:      token,
		TargetModel: model,
		HTTPClient:  a.HTTPClient,
	}

	ch, err := adapter.SendMessageStream(ctx, req)
	if err != nil && (errors.Is(err, types.ErrRateLimitReached) || strings.Contains(err.Error(), "429")) && model != DefaultAGYFlashModel {
		// Fallback to flash if pro quota was hit
		fallbackAdapter := &OpenAICompatibleAdapter{
			AdapterID:   a.AdapterID,
			PriorityLvl: a.PriorityLvl,
			BaseURL:     agyBaseURL,
			APIKey:      token,
			TargetModel: DefaultAGYFlashModel,
			HTTPClient:  a.HTTPClient,
		}
		return fallbackAdapter.SendMessageStream(ctx, req)
	}

	return ch, err
}
