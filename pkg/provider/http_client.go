package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

// defaultHTTPClient is the fallback client used by every adapter's client()
// method when no per-adapter *http.Client override is configured. Sharing
// one client (and therefore one *http.Transport) across all requests lets
// Go's connection pool reuse TCP/TLS connections to the same upstream host
// instead of paying a fresh handshake on every single chat request, which is
// what happened when each adapter allocated `&http.Client{}` inline. The
// transport is a tuned clone of http.DefaultTransport rather than the
// package-global DefaultTransport itself, so this file doesn't reach into
// and mutate shared process-wide state.
var defaultHTTPClient = &http.Client{
	// No total Timeout: ChatGPT/Claude web SSE often exceeds 2 minutes
	// (sentinel + PoW + generation). A 120s cap killed the body mid-stream
	// and Claude Code showed "Waiting for API response / check your network".
	// Keep a finite first-byte bound so dead peers do not occupy a slot forever,
	// but allow slow reasoning / web sentinel flows. Once headers arrive, stream
	// lifetime is governed only by request context.
	Timeout:   0,
	Transport: newDefaultTransport(),
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if strings.Contains(req.URL.Host, "google.com") && strings.Contains(req.URL.Path, "/sorry") {
			return fmt.Errorf("%w: redirected to Google anti-bot CAPTCHA", types.ErrRateLimitReached)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

func newDefaultTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 150
	t.MaxIdleConnsPerHost = 25
	t.IdleConnTimeout = 90 * time.Second
	t.ResponseHeaderTimeout = 5 * time.Minute
	t.ExpectContinueTimeout = 1 * time.Second
	return t
}

// WarmUpConnections sends lightweight concurrent HEAD/OPTIONS requests to
// pre-establish TLS sessions and keep-alive sockets in the connection pool.
// Runs non-blocking in background, zero cost, completely silent on failure.
func WarmUpConnections(endpoints []string) {
	if len(endpoints) == 0 {
		return
	}
	go func() {
		client := &http.Client{
			Transport: defaultHTTPClient.Transport,
			Timeout:   2500 * time.Millisecond,
		}
		for _, ep := range endpoints {
			go func(urlStr string) {
				req, err := http.NewRequest(http.MethodHead, urlStr, nil)
				if err != nil {
					return
				}
				req.Header.Set("User-Agent", "amux/connection-warmup")
				resp, err := client.Do(req)
				if err == nil && resp != nil {
					_ = resp.Body.Close()
				}
			}(ep)
		}
	}()
}
