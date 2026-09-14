package guard

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	proxyClientMu   sync.RWMutex
	proxyClientsMap = make(map[string]*http.Client)
)

// NewProxyTransport builds an http.Transport configured to route all traffic
// through the specified proxyURL (supports http://, https://, socks5://).
func NewProxyTransport(proxyURL string) (*http.Transport, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return http.DefaultTransport.(*http.Transport).Clone(), nil
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("guard: invalid proxy url %q: %w", proxyURL, err)
	}

	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyURL(parsed)
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	// Generation providers may legitimately take several minutes before first
	// SSE bytes (reasoning, sentinel and upstream queueing).
	t.ResponseHeaderTimeout = 5 * time.Minute
	return t, nil
}

// GetClientForProxy returns a cached, shared *http.Client configured with
// the given egress proxy to maximize TCP/TLS connection reuse.
func GetClientForProxy(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil, nil // Caller should use its default client
	}

	proxyClientMu.RLock()
	if c, ok := proxyClientsMap[proxyURL]; ok {
		proxyClientMu.RUnlock()
		return c, nil
	}
	proxyClientMu.RUnlock()

	proxyClientMu.Lock()
	defer proxyClientMu.Unlock()
	// Double check
	if c, ok := proxyClientsMap[proxyURL]; ok {
		return c, nil
	}

	transport, err := NewProxyTransport(proxyURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout:   0, // Streaming requests need unbounded body lifetime
		Transport: transport,
	}
	proxyClientsMap[proxyURL] = client
	return client, nil
}
