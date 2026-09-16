package browser

import (
	"strings"
	"testing"
)

func TestManualLoginHintNamesCookieAndHost(t *testing.T) {
	for _, target := range []WebLoginTarget{ChatGPTWebLogin, ClaudeWebLogin, GeminiWebLogin} {
		hint := ManualLoginHint(target)
		if !strings.Contains(hint, target.CookieName) {
			t.Errorf("%s: hint missing cookie name %q: %s", target.Name, target.CookieName, hint)
		}
		host := trimCookieHost(target.CookieHost)
		if !strings.Contains(hint, host) {
			t.Errorf("%s: hint missing host %q: %s", target.Name, host, hint)
		}
		if strings.Contains(hint, "https://.") {
			t.Errorf("%s: leading dot not trimmed from host: %s", target.Name, hint)
		}
	}
}

func TestTrimCookieHost(t *testing.T) {
	cases := map[string]string{
		".chatgpt.com": "chatgpt.com",
		"claude.ai":    "claude.ai",
		"":             "",
	}
	for in, want := range cases {
		if got := trimCookieHost(in); got != want {
			t.Errorf("trimCookieHost(%q) = %q, want %q", in, got, want)
		}
	}
}
