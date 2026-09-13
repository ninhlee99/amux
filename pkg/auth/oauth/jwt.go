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
