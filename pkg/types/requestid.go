package types

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader carries the gateway's per-request trace id. The gateway
// sets it on the inbound request (so every layer can log it) and on the
// response (so a client-side failure can be matched to gateway.log and
// errors.log). It is stripped before any upstream call.
const RequestIDHeader = "X-Amux-Request-Id"

// NewRequestID returns a short random id such as "ax_3f9a1c07b2de".
func NewRequestID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "ax_" + hex.EncodeToString(b[:])
}

// ValidRequestID accepts a caller-supplied id only when it is short and made
// of [A-Za-z0-9_-], so it can be written to logs verbatim.
func ValidRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		ok := c == '_' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// EnsureRequestID keeps a valid inbound id or assigns a new one, and mirrors
// it on the response headers. It returns the id in use.
func EnsureRequestID(w http.ResponseWriter, r *http.Request) string {
	id := r.Header.Get(RequestIDHeader)
	if !ValidRequestID(id) {
		id = NewRequestID()
		r.Header.Set(RequestIDHeader, id)
	}
	if w != nil {
		w.Header().Set(RequestIDHeader, id)
	}
	return id
}

// RequestIDFrom returns the trace id the gateway assigned to r, or "".
func RequestIDFrom(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Header.Get(RequestIDHeader)
}
