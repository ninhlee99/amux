package oauth

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

func TestParseJWTExpiry(t *testing.T) {
	exp := time.Now().Add(48 * time.Hour).Unix()
	payload := fmt.Sprintf(`{"sub":"user123","exp":%d}`, exp)
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	token := fmt.Sprintf("header.%s.signature", encodedPayload)

	parsedTime, ok := ParseJWTExpiry(token)
	if !ok {
		t.Fatalf("expected ParseJWTExpiry to succeed")
	}
	if parsedTime.Unix() != exp {
		t.Fatalf("expected expiry %d, got %d", exp, parsedTime.Unix())
	}

	// Invalid token
	_, ok = ParseJWTExpiry("not-a-jwt")
	if ok {
		t.Fatalf("expected invalid token to return false")
	}
}
