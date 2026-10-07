package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/tools"
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

// sseStartGrace is how long a streaming request may wait for routing before
// poolSendStreamingLazy opens the client stream.
var sseStartGrace = 5 * time.Second

// poolSendStreamingLazy routes req like poolSendStreaming but leaves the
// response uncommitted until routing outlasts sseStartGrace: open() is
// called then (and before every keepalive), so a fast failure can still be
// answered with an HTTP error status the client retries.
func poolSendStreamingLazy(r *http.Request, pool *router.AccountPoolRouter, req *types.ChatRequest, open, keepalive func()) (<-chan types.StreamChunk, error) {
	type result struct {
		stream <-chan types.StreamChunk
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		stream, err := poolSend(r, pool, req)
		resultCh <- result{stream: stream, err: err}
	}()

	grace := time.NewTimer(sseStartGrace)
	defer grace.Stop()
	ticker := time.NewTicker(streamKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case res := <-resultCh:
			return res.stream, res.err
		case <-grace.C:
			open()
		case <-ticker.C:
			open()
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

// gatewayFailureStatus maps a routing failure to the HTTP status the client
// sees. An unreachable upstream (network down, Cloudflare challenge) or its
// cooldown is 503 with Retry-After matching the router's back-off, which
// Claude Code and the SDKs retry, so the session recovers by itself once
// the network is back.
func gatewayFailureStatus(err error) (status, retryAfterSec int) {
	if err == nil {
		return http.StatusOK, 0
	}
	var rle *types.RateLimitError
	if errors.As(err, &rle) || errors.Is(err, types.ErrRateLimitReached) {
		ra := 60
		if rle != nil && rle.RetryAfter > 0 {
			ra = int(rle.RetryAfter.Seconds())
		}
		return http.StatusTooManyRequests, ra
	}
	msg := err.Error()
	if strings.Contains(msg, "Cloudflare challenge") {
		return http.StatusServiceUnavailable, 30
	}
	if errors.Is(err, types.ErrUpstreamUnreachable) || strings.Contains(msg, "network unreachable") {
		return http.StatusServiceUnavailable, 10
	}
	if errors.Is(err, types.ErrAuthentication) || strings.Contains(msg, "authentication failed") || strings.Contains(msg, "unauthorized") {
		return http.StatusUnauthorized, 0
	}
	if strings.Contains(msg, "rate limit") || strings.Contains(msg, "429") {
		return http.StatusTooManyRequests, 60
	}
	return http.StatusBadGateway, 0
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
//	X-Provider — pool account id (works even when removed from rotate pool),
//	             or an account family such as "gemini:web" / "chatgpt",
//	             in which case amux picks and fails over among its accounts
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
	EnrichRequestMetadata(r, req)
	var ch <-chan types.StreamChunk
	var err error
	if id := explicitProviderHeaders(r, req); id != "" {
		ch, err = pool.SendProvider(r.Context(), id, req)
	} else {
		ch, err = pool.Send(r.Context(), req)
	}
	if err != nil && req != nil {
		switch req.ClientDialect {
		case tools.DialectCodex, tools.DialectCursor:
			if provider.CodexAuthAvailable() {
				term.LogProxy("⚠️ Pool router failed (%v) -> activating fail-safe fallback to direct Codex credentials", err)
				codexAdapter := &provider.CodexCLIAdapter{
					AdapterID:   "keychain:codex",
					TargetModel: req.Model,
				}
				if fallbackStream, ferr := codexAdapter.SendMessageStream(r.Context(), req); ferr == nil && fallbackStream != nil {
					return fallbackStream, nil
				}
			}
		default:
			if tok := auth.LiveKeychainToken(); tok != nil && tok.Access != "" {
				term.LogProxy("⚠️ Pool router failed (%v) -> activating fail-safe fallback to direct Claude credentials", err)
				directAdapter := &provider.ClaudeAdapter{
					AdapterID:   "keychain:direct",
					APIKey:      tok.Access,
					TargetModel: req.Model,
				}
				if fallbackStream, ferr := directAdapter.SendMessageStream(r.Context(), req); ferr == nil && fallbackStream != nil {
					return fallbackStream, nil
				}
			}
		}
	}
	return ch, err
}

// EnrichRequestMetadata populates SessionID and project metadata on req from HTTP headers and client connection.
func EnrichRequestMetadata(r *http.Request, req *types.ChatRequest) {
	if req == nil {
		return
	}
	var project string
	if r != nil {
		for _, h := range []string{
			"X-Project-Root", "x-project-root",
			"X-Project-Dir", "x-project-dir",
			"X-Project", "x-project",
			"X-Cwd", "x-cwd",
			"X-Workspace-Folder", "x-workspace-folder",
		} {
			if val := strings.TrimSpace(r.Header.Get(h)); val != "" {
				project = val
				break
			}
		}
		if project == "" {
			project = usage.ProjectForRemoteAddr(r.RemoteAddr)
		}
	}
	if project != "" {
		if req.Metadata == nil {
			req.Metadata = map[string]any{}
		}
		if _, ok := req.Metadata["project"]; !ok {
			req.Metadata["project"] = project
		}
	}
	if strings.TrimSpace(req.SessionID) == "" {
		if sk := guard.ExtractSessionKey(r, req); sk != "" {
			req.SessionID = sk
		}
	}
}
