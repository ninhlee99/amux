package guard

import (
	"net/http"
	"strings"
)

// proxyInternalHeaders list internal headers used by amux / reverse-proxies
// that could leak proxy presence or routing decisions to upstream providers.
var proxyInternalHeaders = []string{
	"x-provider",
	"x-model",
	"x-amux-auth",
	"x-amux-token",
	"x-amux-route",
	"x-amux-request-id",
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
