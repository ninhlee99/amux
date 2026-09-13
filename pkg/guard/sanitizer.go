package guard

import (
	"net/http"
	"strings"
)

// hopByHopHeaders list standard hop-by-hop headers that should not be forwarded.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// proxyInternalHeaders list internal headers used by amux / reverse-proxies
// that could leak proxy presence or routing decisions to upstream providers.
var proxyInternalHeaders = []string{
	"x-provider",
	"x-model",
	"x-amux-auth",
	"x-amux-token",
	"x-amux-route",
	"x-forwarded-for",
	"x-forwarded-proto",
	"x-forwarded-host",
	"x-forwarded-server",
	"x-forwarded-port",
	"x-real-ip",
	"x-client-ip",
	"via",
	"forwarded",
}

// SanitizeOutboundHeaders scrubs internal routing, proxy tags, and client IP
// leak headers before the HTTP request is dispatched to any upstream LLM service.
// It preserves legitimate client headers (e.g. anthropic-*, authorization, user-agent).
func SanitizeOutboundHeaders(h http.Header) {
	if h == nil {
		return
	}

	for _, k := range proxyInternalHeaders {
		h.Del(k)
		// Also ensure case-insensitive variations are purged
		for existingKey := range h {
			if strings.EqualFold(existingKey, k) {
				h.Del(existingKey)
			}
		}
	}
}

// SanitizeOutboundRequest performs header sanitization on an outbound http.Request.
func SanitizeOutboundRequest(req *http.Request) {
	if req == nil {
		return
	}
	SanitizeOutboundHeaders(req.Header)
}

// EnsureSafeUserAgent ensures the User-Agent header looks natural and does not
// advertise proxy / automated scrapers if empty.
func EnsureSafeUserAgent(h http.Header, fallbackUA string) {
	if h == nil {
		return
	}
	ua := strings.TrimSpace(h.Get("User-Agent"))
	if ua == "" && fallbackUA != "" {
		h.Set("User-Agent", fallbackUA)
	}
}
