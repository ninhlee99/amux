package proxy

import (
	"time"

	"amux-accounts/pkg/ctxshrink"
)

// Re-export constants and types from ctxshrink so proxy callers and tests
// continue to compile without disruption.
const (
	DefaultDeterministicTTL = ctxshrink.DefaultDeterministicTTL
	MaxDeterministicEntries = ctxshrink.MaxDeterministicEntries
)

type CachedReplay = ctxshrink.CachedReplay
type DeterministicReplayCache = ctxshrink.DeterministicReplayCache

// GlobalReplayCache returns the singleton DeterministicReplayCache instance.
func GlobalReplayCache() *ctxshrink.DeterministicReplayCache {
	return ctxshrink.GlobalReplayCache()
}

// NewDeterministicReplayCache creates a new replay cache.
func NewDeterministicReplayCache(ttl time.Duration) *ctxshrink.DeterministicReplayCache {
	return ctxshrink.NewDeterministicReplayCache(ttl)
}
