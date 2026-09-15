package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClaudeAccount is the useful subset of GET https://claude.ai/api/account.
type ClaudeAccount struct {
	Email string
	Plan  string // "pro" | "free"
}

// FetchClaudeAccount loads the signed-in account email and plan from claude.ai using
// a sessionKey (and optional full Cookie header for Cloudflare jars).
func FetchClaudeAccount(sessionKey, cookieHeader string) (*ClaudeAccount, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, fmt.Errorf("empty sessionKey")
	}
	cookie := strings.TrimSpace(cookieHeader)
	if cookie == "" {
		cookie = "sessionKey=" + sessionKey
	} else if !strings.Contains(cookie, "sessionKey=") {
		cookie = "sessionKey=" + sessionKey + "; " + cookie
	}

	req, err := http.NewRequest(http.MethodGet, "https://claude.ai/api/account", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Referer", "https://claude.ai/")
	req.Header.Set("anthropic-client-platform", "web_claude_ai")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("account endpoint status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	email := parseClaudeAccountEmail(body)
	if email == "" {
		return nil, fmt.Errorf("no email in account response")
	}
	plan := parseClaudeAccountPlan(body)
	return &ClaudeAccount{Email: email, Plan: plan}, nil
}

func parseClaudeAccountPlan(body []byte) string {
	var doc struct {
		Memberships []struct {
			Organization struct {
				BillingType               string `json:"billing_type"`
				AnalyticsSubscriptionPlan string `json:"analytics_subscription_plan"`
			} `json:"organization"`
		} `json:"memberships"`
	}
	if err := json.Unmarshal(body, &doc); err == nil {
		for _, m := range doc.Memberships {
			plan := strings.ToLower(m.Organization.AnalyticsSubscriptionPlan)
			billing := strings.ToLower(m.Organization.BillingType)
			switch {
			case plan == "claude_max" || plan == "raven" || strings.Contains(plan, "max"):
				return "max"
			case plan == "claude_pro" || plan == "claude_team" || plan == "claude_enterprise":
				return "pro"
			case strings.Contains(billing, "stripe") && !strings.Contains(plan, "free"):
				// Paid billing without an explicit free label — treat as pro (not free).
				return "pro"
			}
		}
	}
	return "free"
}

func parseClaudeAccountEmail(body []byte) string {
	var top struct {
		EmailAddress string `json:"email_address"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(body, &top); err == nil {
		if e := strings.TrimSpace(top.EmailAddress); e != "" {
			return e
		}
		if e := strings.TrimSpace(top.Email); e != "" {
			return e
		}
	}
	var flat struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && strings.TrimSpace(flat.Email) != "" {
		return strings.TrimSpace(flat.Email)
	}
	var nested struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &nested); err == nil {
		if e := strings.TrimSpace(nested.Account.Email); e != "" {
			return e
		}
		if e := strings.TrimSpace(nested.User.Email); e != "" {
			return e
		}
	}
	return ""
}

