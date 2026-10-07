package types

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureRequestID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	w := httptest.NewRecorder()
	id := EnsureRequestID(w, r)
	if !strings.HasPrefix(id, "ax_") || len(id) != 15 {
		t.Fatalf("generated id %q", id)
	}
	if RequestIDFrom(r) != id || w.Header().Get(RequestIDHeader) != id {
		t.Fatalf("id not mirrored: req=%q resp=%q", RequestIDFrom(r), w.Header().Get(RequestIDHeader))
	}

	r2 := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r2.Header.Set(RequestIDHeader, "client-trace_42")
	if got := EnsureRequestID(httptest.NewRecorder(), r2); got != "client-trace_42" {
		t.Fatalf("valid caller id replaced: %q", got)
	}

	r3 := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	r3.Header.Set(RequestIDHeader, "bad id\ninjected")
	if got := EnsureRequestID(httptest.NewRecorder(), r3); got == "bad id\ninjected" || !strings.HasPrefix(got, "ax_") {
		t.Fatalf("unsafe caller id kept: %q", got)
	}
}
