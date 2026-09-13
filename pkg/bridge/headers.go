package bridge

import (
	"net/http"
	"strings"

	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

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
	if id := explicitProviderHeaders(r, req); id != "" {
		return pool.SendNamed(r.Context(), id, req)
	}
	return pool.Send(r.Context(), req)
}

