package browser

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// GeminiAccount holds account identity metadata loaded from gemini.google.com/app.
type GeminiAccount struct {
	Email string
	Plan  string // "pro" (Gemini Advanced / Google One) | "free"
}

var (
	// Matches WIZ_global_data "oPEP7c": "email@domain.com"
	wizEmailRe = regexp.MustCompile(`"oPEP7c"\s*:\s*"([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})"`)
	// Matches (email@domain.com) inside aria-label="... (email)" on Google account avatar anchor
	ariaLabelEmailRe = regexp.MustCompile(`aria-label="[^"]*?\(([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\)"`)
	// Matches <div>email@domain.com</div> inside Google bar profile card
	htmlTagEmailRe = regexp.MustCompile(`>([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})<`)
	// General email pattern
	generalEmailRe = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

func isSystemOrStaticEmail(email string) bool {
	low := strings.ToLower(email)
	return strings.HasSuffix(low, "@google.com") ||
		strings.Contains(low, "example.com") ||
		strings.Contains(low, "w3.org") ||
		strings.Contains(low, "schema.org")
}

// ParseGeminiAccountEmail extracts the Google account email from the HTML of gemini.google.com/app.
func ParseGeminiAccountEmail(html string) string {
	if m := wizEmailRe.FindStringSubmatch(html); len(m) > 1 {
		cand := strings.TrimSpace(m[1])
		if !isSystemOrStaticEmail(cand) {
			return cand
		}
	}
	if m := ariaLabelEmailRe.FindStringSubmatch(html); len(m) > 1 {
		cand := strings.TrimSpace(m[1])
		if !isSystemOrStaticEmail(cand) {
			return cand
		}
	}
	for _, m := range htmlTagEmailRe.FindAllStringSubmatch(html, -1) {
		if len(m) > 1 {
			cand := strings.TrimSpace(m[1])
			if !isSystemOrStaticEmail(cand) {
				return cand
			}
		}
	}
	for _, m := range generalEmailRe.FindAllString(html, -1) {
		cand := strings.TrimSpace(m)
		if !isSystemOrStaticEmail(cand) {
			return cand
		}
	}
	return ""
}

// ParseGeminiAccountPlan extracts the tier (pro / free) from the HTML of gemini.google.com/app.
func ParseGeminiAccountPlan(html string) string {
	lower := strings.ToLower(html)
	if strings.Contains(lower, "gemini advanced") ||
		strings.Contains(lower, "gói thành viên") ||
		strings.Contains(lower, "\"is_pro\":true") ||
		strings.Contains(lower, "google one") {
		return "pro"
	}
	return "free"
}

// FetchGeminiAccount loads the signed-in account email and plan from gemini.google.com using
// __Secure-1PSID (and optional full Cookie header).
func FetchGeminiAccount(sessionKey, cookieHeader string) (*GeminiAccount, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	cookie := strings.TrimSpace(cookieHeader)
	if cookie == "" && sessionKey != "" {
		cookie = "__Secure-1PSID=" + sessionKey
	} else if sessionKey != "" && !strings.Contains(cookie, "__Secure-1PSID=") {
		cookie = "__Secure-1PSID=" + sessionKey + "; " + cookie
	}
	if !strings.Contains(cookie, "__Secure-1PSIDTS=") {
		if ts, _, _ := ExtractCookie("google.com", "__Secure-1PSIDTS"); ts != "" {
			cookie += "; __Secure-1PSIDTS=" + ts
		}
	}
	if cookie == "" {
		return nil, fmt.Errorf("empty cookie")
	}

	req, err := http.NewRequest(http.MethodGet, "https://gemini.google.com/app", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini init status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	html := string(body)

	email := ParseGeminiAccountEmail(html)
	if email == "" {
		return nil, fmt.Errorf("no email found in gemini page (not logged in?)")
	}
	plan := ParseGeminiAccountPlan(html)
	return &GeminiAccount{Email: email, Plan: plan}, nil
}
