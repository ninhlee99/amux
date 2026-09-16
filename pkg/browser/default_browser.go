package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenDefaultBrowser opens target in the user's existing default browser,
// reusing a running window rather than launching a dedicated instance.
//
// This is the same mechanism the OAuth flows use. It cannot be combined with
// cookie capture: reading cookies requires --remote-debugging-port, which only
// takes effect on a browser amux launches itself (see CaptureWebAuthViaBrowser).
// So callers that open the default browser must ask the user to paste the
// credential back.
func OpenDefaultBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// ManualLoginHint tells the user where to find the cookie for a target after
// signing in, since the default-browser path cannot read it automatically.
//
// The DevTools cookie store is keyed by the site actually open in the tab, not
// by the cookie's own domain, so point the user at StartURL's host rather than
// CookieHost (which is ".google.com" for Gemini, a site they never visit).
func ManualLoginHint(target WebLoginTarget) string {
	host := startURLHost(target)
	return fmt.Sprintf(
		"After signing in, copy the %q cookie:\n"+
			"  DevTools (⌥⌘I / F12) → Application → Cookies → https://%s → %s → copy Value",
		target.CookieName, host, target.CookieName)
}

// startURLHost returns the host of target.StartURL, falling back to CookieHost
// if StartURL cannot be parsed.
func startURLHost(target WebLoginTarget) string {
	if u, err := url.Parse(target.StartURL); err == nil && u.Host != "" {
		return u.Host
	}
	return trimCookieHost(target.CookieHost)
}

func trimCookieHost(host string) string {
	if len(host) > 0 && host[0] == '.' {
		return host[1:]
	}
	return host
}
