package proxy

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithGzip(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","message":"hello world with high speed compression"}`))
	})

	wrapped := withGzip(handler)

	// 1. Client without gzip accept
	req1 := httptest.NewRequest(http.MethodGet, "/_am/status", nil)
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)

	if rec1.Header().Get("Content-Encoding") == "gzip" {
		t.Fatalf("expected uncompressed, got gzip")
	}
	if !strings.Contains(rec1.Body.String(), "hello world") {
		t.Fatalf("unexpected uncompressed body: %s", rec1.Body.String())
	}

	// 2. Client with Accept-Encoding: gzip
	req2 := httptest.NewRequest(http.MethodGet, "/_am/status", nil)
	req2.Header.Set("Accept-Encoding", "gzip, deflate")
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)

	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip content-encoding, got %q", rec2.Header().Get("Content-Encoding"))
	}

	gzReader, err := gzip.NewReader(rec2.Body)
	if err != nil {
		t.Fatalf("failed to decode gzip body: %v", err)
	}
	defer gzReader.Close()
	unzipped, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed to read unzipped data: %v", err)
	}
	if !strings.Contains(string(unzipped), "hello world with high speed compression") {
		t.Fatalf("unzipped content mismatch: %s", string(unzipped))
	}

	// 3. SSE should skip gzip even if client sends Accept-Encoding: gzip
	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: chunk\n\n"))
	})
	wrappedSSE := withGzip(sseHandler)
	req3 := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	req3.Header.Set("Accept-Encoding", "gzip")
	req3.Header.Set("Accept", "text/event-stream")
	rec3 := httptest.NewRecorder()
	wrappedSSE.ServeHTTP(rec3, req3)

	if rec3.Header().Get("Content-Encoding") == "gzip" {
		t.Fatalf("SSE streams must never be gzip-compressed")
	}
}
