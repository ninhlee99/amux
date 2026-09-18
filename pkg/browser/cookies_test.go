package browser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestParseCookieHeader(t *testing.T) {
	tests := []struct {
		raw, name, want string
	}{
		{"sessionKey=abc123", "sessionKey", "abc123"},
		{"Cookie: sessionKey=abc123; other=x", "sessionKey", "abc123"},
		{"a=1; sessionKey=xyz; b=2", "sessionKey", "xyz"},
		{"eyJhbGciOi", "sessionKey", "eyJhbGciOi"}, // bare token
		{"", "sessionKey", ""},
	}
	for _, tt := range tests {
		got := ParseCookieHeader(tt.raw, tt.name)
		if got != tt.want {
			t.Errorf("ParseCookieHeader(%q,%q)=%q want %q", tt.raw, tt.name, got, tt.want)
		}
	}
}

func TestParseCookieHeaderBareValueWithPadding(t *testing.T) {
	// Real __Secure-1PSID / sessionKey values are base64-ish and often carry
	// '=' padding. A bare paste of one must not be rejected.
	cases := map[string]string{
		"g.a000xyzABC123==":  "g.a000xyzABC123==",
		"g.a000xyzABC123=":   "g.a000xyzABC123=",
		"g.a000xyzABC123":    "g.a000xyzABC123",
		"sk-ant-abc123==":    "sk-ant-abc123==",
	}
	for in, want := range cases {
		if got := ParseCookieHeader(in, "__Secure-1PSID"); got != want {
			t.Errorf("ParseCookieHeader(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCookieHeaderStillPrefersNamedPair(t *testing.T) {
	// A genuine name=value pair must still be parsed by name, not returned raw.
	if got := ParseCookieHeader("__Secure-1PSID=abc123", "__Secure-1PSID"); got != "abc123" {
		t.Errorf("named pair = %q, want abc123", got)
	}
	// A pair for a different cookie yields nothing, not the raw string.
	if got := ParseCookieHeader("other=abc123", "__Secure-1PSID"); got != "" {
		t.Errorf("mismatched name = %q, want empty", got)
	}
	// Full header still works.
	hdr := "Cookie: a=1; __Secure-1PSID=wanted; b=2"
	if got := ParseCookieHeader(hdr, "__Secure-1PSID"); got != "wanted" {
		t.Errorf("header = %q, want wanted", got)
	}
}

func TestDecryptCookie_Strips32ByteHeader(t *testing.T) {
	key := []byte("0123456789abcdef") // 16-byte key
	header32 := make([]byte, 32)
	for i := range header32 {
		header32[i] = byte(i + 1)
	}
	expected := "hello-chatgpt-session-token"
	plaintext := append(header32, []byte(expected)...)

	// PKCS#7 pad to block size 16
	padLen := 16 - (len(plaintext) % 16)
	for i := 0; i < padLen; i++ {
		plaintext = append(plaintext, byte(padLen))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte(" "), aes.BlockSize)
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(ciphertext, plaintext)

	encVal := append([]byte("v10"), ciphertext...)
	got := decryptCookie(key, encVal)
	if got != expected {
		t.Errorf("decryptCookie got %q, want %q", got, expected)
	}
}


