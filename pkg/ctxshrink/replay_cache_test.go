package ctxshrink

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestDeterministicReplayCache_FullLifecycle(t *testing.T) {
	cache := NewDeterministicReplayCache(10 * time.Minute)

	reqStream := &types.ChatRequest{
		Model:               "claude-3-7-sonnet-20250219",
		Stream:              true,
		Temperature:         0.0,
		ExplicitTemperature: true,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "check test status"},
		},
	}

	hashStream, ok := cache.ComputeHash(reqStream)
	if !ok || hashStream == "" {
		t.Fatalf("expected hash for explicit temperature 0")
	}

	reqNonStream := &types.ChatRequest{
		Model:               "claude-3-7-sonnet-20250219",
		Stream:              false,
		Temperature:         0.0,
		ExplicitTemperature: true,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "check test status"},
		},
	}

	hashNonStream, ok := cache.ComputeHash(reqNonStream)
	if !ok || hashNonStream == "" {
		t.Fatalf("expected hash for non-stream")
	}

	if hashStream == hashNonStream {
		t.Fatalf("stream and non-stream hashes must differ")
	}

	// Implicit temperature should be uncacheable (defaults to 1.0 stochastic)
	reqImplicit := &types.ChatRequest{
		Model:               "claude-3-7-sonnet-20250219",
		Stream:              true,
		Temperature:         0.0,
		ExplicitTemperature: false,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "check test status"},
		},
	}
	if _, ok := cache.ComputeHash(reqImplicit); ok {
		t.Fatalf("implicit temperature should not be cacheable")
	}

	// Test RecordingWriter
	rec := httptest.NewRecorder()
	rw := NewRecordingWriter(rec)
	rw.Header().Set("Content-Type", "text/event-stream")
	event1 := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	event2 := []byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	rw.Write(event1)
	rw.Write(event2)
	rw.Flush()

	events := rw.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 recorded events, got %d", len(events))
	}

	// Store in cache
	replay := &CachedReplay{
		ContentType:  "text/event-stream",
		SSEEvents:    events,
		InputTokens:  100,
		OutputTokens: 20,
	}
	cache.Put(hashStream, replay)

	// Retrieve
	cached, found := cache.Get(hashStream)
	if !found || len(cached.SSEEvents) != 2 {
		t.Fatalf("failed to retrieve cached replay")
	}

	// Replay to a new response writer
	serveRec := httptest.NewRecorder()
	if err := cached.Serve(serveRec, true); err != nil {
		t.Fatalf("cached.Serve failed: %v", err)
	}
	if serveRec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", serveRec.Header().Get("Content-Type"))
	}
	bodyStr := serveRec.Body.String()
	if bodyStr != string(event1)+string(event2) {
		t.Fatalf("replayed body mismatch: got %q", bodyStr)
	}

	// Test snapshot persistence
	tmpDir, err := os.MkdirTemp("", "amux-cache-test-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	snapPath := filepath.Join(tmpDir, "replay_cache.json")
	if err := cache.SaveSnapshot(snapPath); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	newCache := NewDeterministicReplayCache(10 * time.Minute)
	if err := newCache.LoadSnapshot(snapPath); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	if _, found := newCache.Get(hashStream); !found {
		t.Fatalf("expected to find hashStream after loading snapshot")
	}
}

func TestComputeRawHash_ExplicitTemperature(t *testing.T) {
	cache := NewDeterministicReplayCache(10 * time.Minute)

	rawDeterministic := []byte(`{"model":"gpt-4o","temperature":0,"messages":[{"role":"user","content":"ping"}]}`)
	hash, ok := cache.ComputeRawHash(rawDeterministic)
	if !ok || hash == "" {
		t.Fatalf("expected raw deterministic request to hash")
	}

	rawCreative := []byte(`{"model":"gpt-4o","temperature":0.7,"messages":[{"role":"user","content":"ping"}]}`)
	if _, ok := cache.ComputeRawHash(rawCreative); ok {
		t.Fatalf("creative temperature should not hash")
	}

	rawUnspecified := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}`)
	if _, ok := cache.ComputeRawHash(rawUnspecified); ok {
		t.Fatalf("unspecified temperature should not hash")
	}
}
