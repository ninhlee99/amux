package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"amux-accounts/pkg/types"
)

// isCloudflareChallenge reports whether resp is a Cloudflare challenge page
// rather than an API answer. The marker can sit past the first few KB of the
// page (after an inline SVG logo), so headers are checked first.
func isCloudflareChallenge(resp *http.Response, body []byte) bool {
	if resp != nil {
		if resp.Header.Get("Cf-Mitigated") != "" {
			return true
		}
		html := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html")
		cf := strings.EqualFold(resp.Header.Get("Server"), "cloudflare")
		if html && cf && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable) {
			return true
		}
	}
	low := bytes.ToLower(body)
	for _, m := range []string{"challenge-platform", "cf_chl", "just a moment...", "cf-turnstile"} {
		if bytes.Contains(low, []byte(m)) {
			return true
		}
	}
	return false
}

// upstreamHTTPError turns a failed web response into a one-line error. A
// Cloudflare challenge becomes ErrUpstreamUnreachable; any other HTML page is
// summarized, so its markup (and random tokens that look like "429") never
// reaches logs, the client or the router's error matching.
func upstreamHTTPError(what string, resp *http.Response, body []byte) error {
	if isCloudflareChallenge(resp, body) {
		return fmt.Errorf("%s: HTTP %d Cloudflare challenge — chatgpt.com is blocking requests from this network for now (common right after it drops or the IP/VPN changes): %w",
			what, resp.StatusCode, types.ErrUpstreamUnreachable)
	}
	msg := string(bytes.TrimSpace(body))
	if strings.HasPrefix(strings.ToLower(msg), "<!doctype") || strings.HasPrefix(strings.ToLower(msg), "<html") {
		msg = "HTML error page"
	} else if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return fmt.Errorf("%s status %d: %s", what, resp.StatusCode, msg)
}

// wrapNetworkError marks "cannot reach the provider at all" transport
// failures (DNS, no route, connection refused/reset) as
// ErrUpstreamUnreachable. Timeouts and client cancellation stay as they are:
// a slow provider is the provider's problem, not the network's.
func wrapNetworkError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return err
	}
	var oe *net.OpError
	var de *net.DNSError
	if errors.As(err, &oe) || errors.As(err, &de) {
		return fmt.Errorf("%w: %w", types.ErrUpstreamUnreachable, err)
	}
	return err
}
