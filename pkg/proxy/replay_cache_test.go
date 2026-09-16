package proxy

import (
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestDeterministicReplayCache_HitAndMiss(t *testing.T) {
	cache := NewDeterministicReplayCache(10 * time.Minute)

	req := &types.ChatRequest{
		Model:       "claude-3-7-sonnet-20250219",
		Temperature: 0,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "fmt.Println(\"hello\")"},
		},
	}

	hash, ok := cache.ComputeHash(req)
	if !ok || hash == "" {
		t.Fatalf("expected hash for temperature 0")
	}

	// Miss
	if _, found := cache.Get(hash); found {
		t.Fatalf("expected cache miss initially")
	}

	// Put
	replay := &CachedReplay{
		ContentType: "application/json",
		Body:        []byte(`{"id":"msg_123","content":[{"type":"text","text":"hello"}]}`),
	}
	cache.Put(hash, replay)

	// Hit
	got, found := cache.Get(hash)
	if !found || string(got.Body) != string(replay.Body) {
		t.Fatalf("expected cache hit with exact body")
	}

	// High temperature should not be cacheable
	reqHighTemp := &types.ChatRequest{
		Model:       "claude-3-7-sonnet-20250219",
		Temperature: 0.7,
		Messages: []types.ChatMessage{
			{Role: "user", Content: "fmt.Println(\"hello\")"},
		},
	}
	_, okHigh := cache.ComputeHash(reqHighTemp)
	if okHigh {
		t.Fatalf("expected high temperature request to be uncacheable")
	}
}
