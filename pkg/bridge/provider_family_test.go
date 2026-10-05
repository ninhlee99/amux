package bridge_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type familyAdapter struct {
	id   string
	prio int
	err  error
}

func (a familyAdapter) ID() string    { return a.id }
func (a familyAdapter) Priority() int { return a.prio }
func (a familyAdapter) SendMessageStream(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if a.err != nil {
		return nil, a.err
	}
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{ID: a.id, Content: "served by " + a.id}
	ch <- types.StreamChunk{ID: a.id, FinishReason: "stop", Done: true}
	close(ch)
	return ch, nil
}

// X-Provider with an account family lets amux choose the account itself.
func TestXProviderFamily_ChatCompletions(t *testing.T) {
	all := []types.ProviderAdapter{
		familyAdapter{id: "groq:api:77", prio: 1},
		familyAdapter{id: "gemini:web:77", prio: 22, err: types.ErrAuthentication},
		familyAdapter{id: "gemini:web:78", prio: 23},
	}
	pool := router.NewAccountPoolRouter(all)
	pool.SetDirectory(all)

	body := []byte(`{"model":"auto","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("X-Provider", "gemini:web")
	w := httptest.NewRecorder()
	bridge.HandleChatCompletions(w, req, pool)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "served by gemini:web:78") {
		t.Fatalf("expected the healthy gemini:web account, got %s", w.Body.String())
	}
}
