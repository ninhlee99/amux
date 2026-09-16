package browser

import (
	"fmt"
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
func ManualLoginHint(target WebLoginTarget) string {
	return fmt.Sprintf(
		"After signing in, copy the %q cookie:\n"+
			"  DevTools (⌥⌘I / F12) → Application → Cookies → https://%s → %s → copy Value",
		target.CookieName, trimCookieHost(target.CookieHost), target.CookieName)
}

func trimCookieHost(host string) string {
	if len(host) > 0 && host[0] == '.' {
		return host[1:]
	}
	return host
}
