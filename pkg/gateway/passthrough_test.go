package gateway_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"amux-accounts/pkg/gateway"
)

func TestDialectsMatch(t *testing.T) {
	tests := []struct {
		client string
		target string
		want   bool
	}{
		{"claude", "claude", true},
		{"claude", "anthropic", true},
		{"anthropic", "claude", true},
		{"codex", "openai", true},
		{"cursor", "openai", true},
		{"gemini", "gemini", true},
		{"gemini", "agy", true},
		{"claude", "openai", false},
		{"codex", "anthropic", false},
		{"gemini", "openai", false},
	}

	for _, tt := range tests {
		got := gateway.DialectsMatch(tt.client, tt.target)
		if got != tt.want {
			t.Errorf("DialectsMatch(%q, %q) = %v, want %v", tt.client, tt.target, got, tt.want)
		}
	}
}

func TestGateway_1to1BitwisePassthroughFidelity(t *testing.T) {
	// Raw SSE chunk payload representing native Anthropic tool execution
	rawSSE := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"model\":\"claude-3-7-sonnet-20250219\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"Bash\",\"input\":{\"command\":\"ls -la\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	// Upstream test server returning exact raw byte stream
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Custom-Header", "preserve-this")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		// Send in chunks to ensure chunk boundaries are preserved
		w.Write(rawSSE[:len(rawSSE)/2])
		if ok {
			flusher.Flush()
		}
		w.Write(rawSSE[len(rawSSE)/2:])
		if ok {
			flusher.Flush()
		}
	}))
	defer upstreamServer.Close()

	// Client request
	clientReqBody := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":true,"messages":[{"role":"user","content":"Run ls"}]}`)
	clientReq := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(clientReqBody))
	clientReq.Header.Set("Content-Type", "application/json")
	clientReq.Header.Set("User-Agent", "Claude-Code/1.0.0")

	recorder := httptest.NewRecorder()

	// Execute 1:1 bitwise passthrough
	err := gateway.TransparentPassthrough(recorder, clientReq, upstreamServer.URL, "test-token", "claude", "claude", "acc-sub-1")
	if err != nil {
		t.Fatalf("TransparentPassthrough error: %v", err)
	}

	res := recorder.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	if res.Header.Get("X-Custom-Header") != "preserve-this" {
		t.Errorf("custom upstream header was not preserved")
	}

	receivedBytes := recorder.Body.Bytes()
	if !bytes.Equal(receivedBytes, rawSSE) {
		t.Fatalf("received bytes do not match upstream byte-for-byte:\nGot:\n%s\nWant:\n%s",
			string(receivedBytes), string(rawSSE))
	}
}
