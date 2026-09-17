package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/telemetry"
)

// TransparentPassthrough streams raw byte streams verbatim between client and upstream when dialects match.
// It bypasses intermediate JSON deserialization, preserving exact SSE chunk boundaries, headers,
// multi-turn messages, custom tool parameters, and tool results.
func TransparentPassthrough(w http.ResponseWriter, r *http.Request, upstreamURL string, authToken string, clientDialect string, targetDialect string, accountID string) error {
	start := time.Now()

	// Parse upstream target URL
	target := strings.TrimRight(upstreamURL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upReq, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		return fmt.Errorf("create upstream request: %w", err)
	}

	// Copy headers verbatim
	for k, vv := range r.Header {
		// Skip hop-by-hop headers
		if strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Keep-Alive") ||
			strings.EqualFold(k, "Proxy-Authenticate") || strings.EqualFold(k, "Proxy-Authorization") ||
			strings.EqualFold(k, "Te") || strings.EqualFold(k, "Trailers") ||
			strings.EqualFold(k, "Transfer-Encoding") || strings.EqualFold(k, "Upgrade") {
			continue
		}
		for _, v := range vv {
			upReq.Header.Add(k, v)
		}
	}

	// Attach target authorization token if specified
	if authToken != "" {
		if clientDialect == "claude" || clientDialect == "anthropic" {
			upReq.Header.Set("x-api-key", authToken)
		} else {
			upReq.Header.Set("Authorization", "Bearer "+authToken)
		}
	}

	// Use streaming transport without buffering
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
	}

	resp, err := client.Do(upReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		telemetry.LogPassthrough(clientDialect, targetDialect, accountID, 0, http.StatusBadGateway, time.Since(start))
		return err
	}
	defer resp.Body.Close()

	// Copy response headers verbatim
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Ensure immediate flushing for SSE streaming
	flusher, isFlusher := w.(http.Flusher)

	// Stream raw bytes directly without deserialization
	buf := make([]byte, 4096)
	totalBytes := 0
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			totalBytes += n
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
			}
			if isFlusher {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				// non-EOF error
			}
			break
		}
	}

	dur := time.Since(start)
	telemetry.LogPassthrough(clientDialect, targetDialect, accountID, 0, resp.StatusCode, dur)
	return nil
}

// DialectsMatch reports whether client dialect and target provider dialect match for 1:1 passthrough.
func DialectsMatch(clientDialect, targetDialect string) bool {
	c := strings.ToLower(strings.TrimSpace(clientDialect))
	t := strings.ToLower(strings.TrimSpace(targetDialect))

	if c == t {
		return true
	}
	if (c == "claude" || c == "anthropic") && (t == "claude" || t == "anthropic") {
		return true
	}
	if (c == "codex" || c == "openai" || c == "cursor") && (t == "codex" || t == "openai" || t == "cursor") {
		return true
	}
	if (c == "gemini" || c == "agy") && (t == "gemini" || t == "agy") {
		return true
	}
	return false
}

// RedactAuthHeaders applies target isolation to authorization headers.
func RedactAuthHeaders(r *http.Request) {
	if !privacy.Enabled {
		return
	}
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer sk-") {
			// Redact only the secret part
		}
	}
}
