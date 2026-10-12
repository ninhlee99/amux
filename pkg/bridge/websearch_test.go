package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

type searchAnswerAdapter struct{ got *types.ChatRequest }

func (a *searchAnswerAdapter) ID() string    { return "chatgpt:search" }
func (a *searchAnswerAdapter) Priority() int { return 1 }
func (a *searchAnswerAdapter) SendMessageStream(_ context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	a.got = req
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{Content: "IANA reserves example.com.\n\n[IANA-managed Reserved Domains](https://www.iana.org/domains/reserved)\nhttps://example.com/"}
	ch <- types.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func TestExtractWebSources(t *testing.T) {
	got := extractWebSources("See [A](https://a.example/x). Also https://b.example/y, and [A again](https://a.example/x)")
	if len(got) != 2 || got[0].Title != "A" || got[0].URL != "https://a.example/x" || got[1].URL != "https://b.example/y" {
		t.Fatalf("got %+v", got)
	}
}

// Claude Code's WebSearch side request gets server_tool_use +
// web_search_tool_result (with each source's title and url) + text.
func TestHandleClaudeMessages_WebSearch(t *testing.T) {
	resetPacingForTest(t)
	backend := &searchAnswerAdapter{}
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{backend})
	body := []byte(`{"model":"claude-sonnet-5-5","max_tokens":100,"stream":false,
		"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],
		"messages":[{"role":"user","content":[{"type":"text","text":"Perform a web search for the query: IANA example domain"}]}]}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	if err := HandleClaudeMessages(w, r, pool, body); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	if len(resp.Content) != 3 || resp.Content[0]["type"] != "server_tool_use" || resp.Content[1]["type"] != "web_search_tool_result" {
		t.Fatalf("unexpected blocks: %s", w.Body.String())
	}
	if q := resp.Content[0]["input"].(map[string]any)["query"]; q != "IANA example domain" {
		t.Errorf("query = %v", q)
	}
	results := resp.Content[1]["content"].([]any)
	first := results[0].(map[string]any)
	if len(results) != 2 || first["title"] != "IANA-managed Reserved Domains" || first["url"] != "https://www.iana.org/domains/reserved" {
		t.Errorf("results = %v", results)
	}
	if len(backend.got.Tools) != 0 || !strings.Contains(backend.got.Messages[len(backend.got.Messages)-1].Content, "markdown link") {
		t.Errorf("web model must get the search instruction without the server tool: %+v", backend.got)
	}
}

type textAdapter struct{ id, text string }

func (a *textAdapter) ID() string    { return a.id }
func (a *textAdapter) Priority() int { return 1 }
func (a *textAdapter) SendMessageStream(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, 2)
	ch <- types.StreamChunk{Content: a.text}
	ch <- types.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

// A pinned account that cannot search (claude.ai without web search) gets
// its WebSearch side request answered by an account that searches natively.
func TestHandleClaudeMessages_WebSearchFallsBack(t *testing.T) {
	resetPacingForTest(t)
	pool := router.NewAccountPoolRouter([]types.ProviderAdapter{
		&textAdapter{id: "claude:web:x", text: "I don't have web search access in this environment."},
		&textAdapter{id: "chatgpt:y", text: "Found it. [Example Domains - IANA](https://www.iana.org/help/example-domains)"},
	})
	body := []byte(`{"model":"m","max_tokens":10,"tools":[{"type":"web_search_20250305","name":"web_search"}],
		"messages":[{"role":"user","content":"Perform a web search for the query: IANA example domain"}]}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	r.Header.Set("X-Provider", "claude:web:x")
	if err := HandleClaudeMessages(w, r, pool, body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), `"url":"https://www.iana.org/help/example-domains"`) {
		t.Fatalf("want the ChatGPT result with its link, got %s", w.Body.String())
	}
}
