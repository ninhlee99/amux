package provider

import (
	"fmt"
	"strings"

	"amux-accounts/pkg/browser"
	"amux-accounts/pkg/types"
)

// checkSessionAccount rejects a refreshed web session signed in to a different
// account than the adapter was configured for. Browser jars are shared, so
// "refresh from browser" can otherwise hand account A's slot account B's
// cookies: both pool entries then hit B and A's limits are never used. An
// unknown email on either side is allowed.
func checkSessionAccount(adapterID, want, got string) error {
	want, got = strings.TrimSpace(want), strings.TrimSpace(got)
	if want == "" || got == "" || want == "-" || strings.EqualFold(want, got) {
		return nil
	}
	return fmt.Errorf("%s: %w: browser session belongs to %s, not %s (run: amux login for %s)",
		adapterID, types.ErrAuthentication, got, want, want)
}

// browserSession is a cookie jar found in one browser profile.
type browserSession struct {
	Value string // the session cookie itself
	Jar   string // Cookie header: session cookie plus companions from the same profile
}

// browserSessionsFor lists sessionCookie from every browser profile (most
// recently used first) with its companion cookies from that same profile.
var browserSessionsFor = func(domain, sessionCookie string, companions ...string) []browserSession {
	var out []browserSession
	for _, c := range browser.ExtractCookies(domain, sessionCookie) {
		jar := sessionCookie + "=" + c.Value
		for _, name := range companions {
			if v := browser.ExtractCookieFromStore(c.Store, domain, name); v != "" {
				jar += "; " + name + "=" + v
			}
		}
		out = append(out, browserSession{Value: c.Value, Jar: jar})
	}
	return out
}

// pickBrowserSession returns the first browser session whose account (as
// reported by whoami) is want. With want unknown the first session that
// whoami accepts wins. Several accounts of one product can be signed in to
// different browser profiles; each amux slot must get its own.
func pickBrowserSession(adapterID, want string, sessions []browserSession, whoami func(browserSession) (string, error)) (browserSession, error) {
	var seen []string
	for _, s := range sessions {
		email, err := whoami(s)
		if err != nil {
			continue
		}
		if checkSessionAccount(adapterID, want, email) == nil {
			return s, nil
		}
		seen = append(seen, email)
	}
	if len(seen) > 0 {
		return browserSession{}, fmt.Errorf("%s: %w: no browser profile is signed in to %s (found %s; run: amux login for %s)",
			adapterID, types.ErrAuthentication, want, strings.Join(seen, ", "), want)
	}
	return browserSession{}, fmt.Errorf("%s: %w: no usable browser session found", adapterID, types.ErrAuthentication)
}
