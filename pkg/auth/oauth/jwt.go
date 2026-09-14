package oauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// ParseJWTEmail extracts the email claim from an unverified JWT ID token.
func ParseJWTEmail(jwtToken string) string {
	parts := strings.Split(jwtToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	// Standard unpadded base64url decoding
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		// Try standard with padding if needed
		decoded, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if email, ok := claims["email"].(string); ok {
		return strings.TrimSpace(email)
	}
	if sub, ok := claims["sub"].(string); ok {
		return strings.TrimSpace(sub)
	}
	return ""
}

// ParseCodexClaims extracts email, chatgpt_account_id, and plan tier from an unverified OpenAI JWT token.
func ParseCodexClaims(jwtToken string) (email, accountID, plan string) {
	parts := strings.Split(jwtToken, ".")
	if len(parts) < 2 {
		return "", "", "free"
	}
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return "", "", "free"
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", "", "free"
	}
	if e, ok := claims["email"].(string); ok {
		email = strings.TrimSpace(e)
	}
	if act, ok := claims["chatgpt_account_id"].(string); ok {
		accountID = strings.TrimSpace(act)
	}
	if pt, ok := claims["chatgpt_plan_type"].(string); ok {
		plan = strings.ToLower(strings.TrimSpace(pt))
	}
	if authObj, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if act, ok := authObj["chatgpt_account_id"].(string); ok && accountID == "" {
			accountID = strings.TrimSpace(act)
		}
		if act, ok := authObj["account_id"].(string); ok && accountID == "" {
			accountID = strings.TrimSpace(act)
		}
		if e, ok := authObj["email"].(string); ok && email == "" {
			email = strings.TrimSpace(e)
		}
		if pt, ok := authObj["chatgpt_plan_type"].(string); ok && plan == "" {
			plan = strings.ToLower(strings.TrimSpace(pt))
		}
	}
	switch plan {
	case "plus", "pro", "team", "business", "enterprise", "edu", "edu_plus", "edu_pro":
		plan = "pro"
	default:
		plan = "free"
	}
	return email, accountID, plan
}

