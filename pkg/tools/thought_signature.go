package tools

import (
	"sync"
	"time"
)

type signatureEntry struct {
	signature string
	expiresAt time.Time
}

var (
	sigMu      sync.RWMutex
	sigCache   = make(map[string]signatureEntry)
	maxEntries = 2000
)

// RecordThoughtSignature caches an opaque thought_signature for a given tool call ID.
// This allows subsequent conversation turns echoing the tool call to attach the required
// signature for Gemini 3.x models without losing reasoning state.
func RecordThoughtSignature(callID, signature string) {
	if callID == "" || signature == "" {
		return
	}
	sigMu.Lock()
	defer sigMu.Unlock()

	// Prune expired entries if cache is large
	if len(sigCache) >= maxEntries {
		now := time.Now()
		for k, v := range sigCache {
			if now.After(v.expiresAt) {
				delete(sigCache, k)
			}
		}
		if len(sigCache) >= maxEntries {
			// Evict older entries
			count := 0
			for k := range sigCache {
				delete(sigCache, k)
				count++
				if count > 400 {
					break
				}
			}
		}
	}

	sigCache[callID] = signatureEntry{
		signature: signature,
		expiresAt: time.Now().Add(2 * time.Hour),
	}
}

// LookupThoughtSignature retrieves the cached thought_signature for a given tool call ID.
func LookupThoughtSignature(callID string) string {
	if callID == "" {
		return ""
	}
	sigMu.RLock()
	defer sigMu.RUnlock()

	entry, ok := sigCache[callID]
	if !ok || time.Now().After(entry.expiresAt) {
		return ""
	}
	return entry.signature
}
