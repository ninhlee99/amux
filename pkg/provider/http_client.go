package provider

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
	Transport: idleStreamTransport{base: newDefaultTransport(), idle: streamIdleTimeout},
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
	t.HTTP2 = webHTTP2Config()
	return t
}

// webHTTP2Config health-checks idle HTTP/2 connections. A silently dropped
// connection (IPv6/NAT) otherwise keeps taking new requests, each waiting out
// the OS TCP timeout: ChatGPT sentinel calls hung ~16 min before
// "read: operation timed out".
func webHTTP2Config() *http.HTTP2Config {
	return &http.HTTP2Config{SendPingTimeout: 30 * time.Second, PingTimeout: 15 * time.Second}
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

// streamIdleTimeout bounds the silence inside a response body. Web SSE streams
// may run long, but claude.ai has left a completion open with no bytes for 25
// minutes; Claude Code then sat on "Mustering…" until its own cancel.
const streamIdleTimeout = 3 * time.Minute

// errStreamIdle is what a body read returns once the watchdog closed it.
var errStreamIdle = errors.New("upstream stream idle: no data received for 3m")

// idleStreamTransport wraps response bodies so a read that sees no bytes for
// `idle` fails instead of blocking forever. Total stream length stays
// unbounded — only silence is limited.
type idleStreamTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t idleStreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || t.idle <= 0 {
		return resp, err
	}
	resp.Body = newIdleBody(resp.Body, t.idle)
	return resp, nil
}

type idleBody struct {
	rc    io.ReadCloser
	timer *time.Timer
	idle  time.Duration
	mu    sync.Mutex
	fired bool
}

func newIdleBody(rc io.ReadCloser, idle time.Duration) *idleBody {
	b := &idleBody{rc: rc, idle: idle}
	b.timer = time.AfterFunc(idle, func() {
		b.mu.Lock()
		b.fired = true
		b.mu.Unlock()
		_ = rc.Close()
	})
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF {
		b.mu.Lock()
		fired := b.fired
		b.mu.Unlock()
		if fired {
			return n, errStreamIdle
		}
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.rc.Close()
}
