package bridge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
	"amux-accounts/pkg/usage"
)

var streamKeepaliveInterval = 15 * time.Second

// beginSSE commits an SSE response before provider setup. Browser-backed
// providers can spend minutes on a challenge or queue before their first
// token; committing and flushing here prevents clients from mistaking that
// wait for a dead proxy connection.
func beginSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	fmt.Fprint(w, ": amux stream connected\n\n")
	flusher.Flush()
	return flusher, true
}

func commentKeepalive(w http.ResponseWriter, flusher http.Flusher) func() {
	return func() {
		fmt.Fprint(w, ": amux upstream pending\n\n")
		flusher.Flush()
	}
}

func drainStream(stream <-chan types.StreamChunk) {
	if stream == nil {
		return
	}
	go func() {
		for range stream {
		}
	}()
}

// recvStreamChunk waits for the next upstream chunk while emitting keepalives
// so downstream SSE clients do not treat a quiet generation gap as a dead proxy.
func recvStreamChunk(ctx context.Context, stream <-chan types.StreamChunk, ping <-chan time.Time, onPing func()) (types.StreamChunk, bool, error) {
	for {
		select {
		case <-ctx.Done():
			return types.StreamChunk{}, false, ctx.Err()
		case <-ping:
			if onPing != nil {
				onPing()
			}
		case chunk, ok := <-stream:
			return chunk, ok, nil
		}
	}
}

// poolSendStreaming keeps downstream SSE alive while an upstream adapter is
// still connecting or waiting for its first token. Only handler goroutine
// writes ResponseWriter; worker goroutine only performs provider setup.
// keepalive may be nil (SSE comments). On client cancel, the worker is waited
// and any unused stream is drained so producers cannot block forever.
func poolSendStreaming(w http.ResponseWriter, r *http.Request, pool *router.AccountPoolRouter, req *types.ChatRequest, flusher http.Flusher, keepalive func()) (<-chan types.StreamChunk, error) {
	if keepalive == nil {
		keepalive = commentKeepalive(w, flusher)
	}
	type result struct {
		stream <-chan types.StreamChunk
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		stream, err := poolSend(r, pool, req)
		resultCh <- result{stream: stream, err: err}
	}()

	ticker := time.NewTicker(streamKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case res := <-resultCh:
			return res.stream, res.err
		case <-ticker.C:
			keepalive()
		case <-r.Context().Done():
			res := <-resultCh
			drainStream(res.stream)
			if res.err != nil {
				return nil, res.err
			}
			return nil, r.Context().Err()
		}
	}
}

// btwDrainer is a pluggable func so tests and the real proxy can provide
// different implementations. The proxy sets this via SetBtwDrainer on startup.
// Default is nil (no-op) so the bridge package has no import cycle with proxy.
var btwDrainer func() []string

// SetBtwDrainer registers the function used to drain /btw messages before
// each LLM request. Called once by the proxy server on startup.
func SetBtwDrainer(fn func() []string) {
	btwDrainer = fn
}

// injectBtwMessages appends any pending /btw messages as a note to the last
// user message in the conversation. Modifies req.Messages in place only when
// there are pending messages, leaving all other turns unchanged.
func injectBtwMessages(req *types.ChatRequest) {
	if btwDrainer == nil || req == nil {
		return
	}
	msgs := btwDrainer()
	if len(msgs) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("\n\n[BTW — user note while you were working]\n")
	for i, m := range msgs {
		sb.WriteString(strings.TrimSpace(m))
		if i < len(msgs)-1 {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("\n[/BTW — please acknowledge and continue your current task]\n")
	note := sb.String()

	// Inject into the last user message, or append a new user turn.
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			req.Messages[i].Content += note
			return
		}
	}
	// No user message found — add one (edge case in pure tool-only turns).
	req.Messages = append(req.Messages, types.ChatMessage{
		Role:    "user",
		Content: strings.TrimLeft(note, "\n"),
	})
}

// explicitProviderHeaders reads optional routing overrides:
//
//	X-Provider — pool account id (works even when removed from rotate pool)
//	X-Model    — override request model for this call
//
// Empty provider → caller keeps default Send() / current behavior.
func explicitProviderHeaders(r *http.Request, req *types.ChatRequest) (providerID string) {
	providerID = strings.TrimSpace(r.Header.Get("X-Provider"))
	if providerID == "" {
		providerID = strings.TrimSpace(r.Header.Get("x-provider"))
	}
	model := strings.TrimSpace(r.Header.Get("X-Model"))
	if model == "" {
		model = strings.TrimSpace(r.Header.Get("x-model"))
	}
	if model != "" && req != nil {
		req.Model = model
	}
	return providerID
}

func poolSend(r *http.Request, pool *router.AccountPoolRouter, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if privacy.Enabled {
		if res := privacy.RedactChatRequest(req); res.Len() > 0 {
			dialect := "privacy"
			if req != nil && req.ClientDialect != "" {
				dialect = req.ClientDialect
			}
			privacy.LogHits(r, res, dialect)
		}
	}
	injectBtwMessages(req)
	if req != nil && strings.TrimSpace(req.SessionID) == "" {
		if sk := guard.ExtractSessionKey(r, req); sk != "" {
			req.SessionID = sk
		}
	}
	if req != nil {
		if root := usage.ProjectForRemoteAddr(r.RemoteAddr); root != "" {
			if req.Metadata == nil {
				req.Metadata = map[string]any{}
			}
			if _, ok := req.Metadata["project"]; !ok {
				req.Metadata["project"] = root
			}
		}
	}
	if id := explicitProviderHeaders(r, req); id != "" {
		return pool.SendNamed(r.Context(), id, req)
	}
	return pool.Send(r.Context(), req)
}
