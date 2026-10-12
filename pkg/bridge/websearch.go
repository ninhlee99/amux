package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

// Claude Code's WebSearch tool sends a side request whose only tool is
// Anthropic's server-side web_search ({"type":"web_search_20250305"}). Web
// accounts cannot run that tool, but ChatGPT, Gemini and claude.ai search the
// web themselves. amux asks them to, then answers in the shape Claude Code
// reads: server_tool_use + web_search_tool_result (title/url per source) +
// text. Without the result block Claude Code gets no links at all.

const webSearchInstruction = "\n\nSearch the web now to answer this. Reply with a short answer, then list every source you used, one per line, as a markdown link: [Page title](https://full.url)."

var (
	reMarkdownLink = regexp.MustCompile(`\[([^\]\n]{1,300})\]\((https?://[^\s)]+)\)`)
	reBareURL      = regexp.MustCompile(`https?://[^\s)\]>"']+`)
	reSearchQuery  = regexp.MustCompile(`(?i)perform a web search for the query:\s*(.+)`)
)

type webSearchResult struct {
	Title string
	URL   string
}

// webSearchQuery reports whether body is a server-side web_search request and
// returns the query it asks for.
func webSearchQuery(body []byte, req *types.ChatRequest) (string, bool) {
	var wrap struct {
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return "", false
	}
	found := false
	for _, t := range wrap.Tools {
		if strings.HasPrefix(t.Type, "web_search_") {
			found = true
		}
	}
	if !found || req == nil {
		return "", false
	}
	q := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			q = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	if m := reSearchQuery.FindStringSubmatch(q); len(m) > 1 {
		q = strings.TrimSpace(m[1])
	}
	return q, true
}

// extractWebSources lists the distinct links in an answer, markdown links
// first (they carry titles), then bare URLs.
func extractWebSources(text string) []webSearchResult {
	var out []webSearchResult
	seen := map[string]bool{}
	add := func(title, u string) {
		u = strings.TrimRight(u, ".,;:")
		if seen[u] {
			return
		}
		seen[u] = true
		if strings.TrimSpace(title) == "" {
			title = u
		}
		out = append(out, webSearchResult{Title: strings.TrimSpace(title), URL: u})
	}
	for _, m := range reMarkdownLink.FindAllStringSubmatch(text, -1) {
		add(m[1], m[2])
	}
	for _, u := range reBareURL.FindAllString(text, -1) {
		add("", u)
	}
	return out
}

// webSearchBlocks builds the Anthropic content blocks for a search answer.
func webSearchBlocks(query, text string) []map[string]any {
	toolID := fmt.Sprintf("srvtoolu_%d", time.Now().UnixNano())
	results := []map[string]any{}
	for _, s := range extractWebSources(text) {
		results = append(results, map[string]any{
			"type": "web_search_result", "title": s.Title, "url": s.URL,
			"encrypted_content": "", "page_age": nil,
		})
	}
	return []map[string]any{
		{"type": "server_tool_use", "id": toolID, "name": "web_search", "input": map[string]any{"query": query}},
		{"type": "web_search_tool_result", "tool_use_id": toolID, "content": results},
		{"type": "text", "text": text},
	}
}

// webSearchFamilies are account families whose web UI searches by itself.
var webSearchFamilies = []string{"chatgpt", "gemini:web"}

// searchElsewhere runs the search on another family that searches natively
// and returns its answer when that answer cites sources.
func searchElsewhere(r *http.Request, pool *router.AccountPoolRouter, req *types.ChatRequest) (string, string) {
	served := strings.ToLower(req.ServingAccount)
	for _, fam := range webSearchFamilies {
		if strings.HasPrefix(served, fam) {
			continue
		}
		cloned := *req
		cloned.SessionID = ""
		ch, err := pool.SendProvider(r.Context(), fam, &cloned)
		if err != nil {
			continue
		}
		var b strings.Builder
		failed := false
		for c := range ch {
			if c.Error != nil {
				failed = true
			}
			b.WriteString(c.Content)
		}
		if ans := strings.TrimSpace(b.String()); !failed && len(extractWebSources(ans)) > 0 {
			return ans, cloned.ServingAccount
		}
	}
	return "", ""
}

// handleWebSearch answers a web_search side request from the pool.
func handleWebSearch(w http.ResponseWriter, r *http.Request, pool *router.AccountPoolRouter, req *types.ChatRequest, query, msgID string) error {
	started := time.Now()
	var kept []types.ToolDef
	for _, t := range req.Tools {
		if t.Name != "web_search" {
			kept = append(kept, t)
		}
	}
	req.Tools = kept
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			req.Messages[i].Content += webSearchInstruction
			break
		}
	}

	var flusher http.Flusher
	if req.Stream {
		f, err := beginAnthropicSSE(w, req, msgID)
		if err != nil {
			return err
		}
		flusher = f
	}
	fail := func(err error) error {
		logChatRequest(r, pool, req, "", "", err.Error(), 0, 0, started, nil)
		if flusher != nil {
			writeAnthropicSSEError(w, flusher, err)
		} else {
			status, _ := gatewayFailureStatus(err)
			writeAnthropicJSONError(w, status, "api_error", err.Error())
		}
		return err
	}

	type result struct {
		stream <-chan types.StreamChunk
		err    error
	}
	resCh := make(chan result, 1)
	go func() {
		s, err := poolSend(r, pool, req)
		resCh <- result{s, err}
	}()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	var res result
wait:
	for {
		select {
		case res = <-resCh:
			break wait
		case <-ping.C:
			if flusher != nil {
				writeAnthropicSSEPing(w, flusher)
			}
		case <-r.Context().Done():
			go func() {
				if got := <-resCh; got.stream != nil {
					drainStream(got.stream)
				}
			}()
			return r.Context().Err()
		}
	}
	if res.err != nil {
		return fail(res.err)
	}
	var text strings.Builder
	for chunk := range res.stream {
		if chunk.Error != nil {
			return fail(chunk.Error)
		}
		text.WriteString(chunk.Content)
	}
	answer := strings.TrimSpace(text.String())
	if len(extractWebSources(answer)) == 0 {
		// The serving account could not search (claude.ai without its web
		// search feature answers "no web access"). The search is a
		// stateless side request, so ask an account that searches natively.
		if alt, from := searchElsewhere(r, pool, req); alt != "" {
			log.Printf("amux: web search: %s returned no sources — answered by %s", req.ServingAccount, from)
			answer = alt
		}
	}
	blocks := webSearchBlocks(query, answer)
	logChatRequest(r, pool, req, answer, "end_turn", "", estimateInputTokens(req), len(answer)/4, started, nil)

	if flusher == nil {
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{
			"id": msgID, "type": "message", "role": "assistant", "model": req.Model,
			"content": blocks, "stop_reason": "end_turn", "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": estimateInputTokens(req), "output_tokens": len(answer) / 4,
				"server_tool_use": map[string]int{"web_search_requests": 1}},
		})
	}
	ev := func(name string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	for i, b := range blocks {
		switch b["type"] {
		case "server_tool_use":
			start := map[string]any{"type": "server_tool_use", "id": b["id"], "name": "web_search", "input": map[string]any{}}
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": start})
			in, _ := json.Marshal(b["input"])
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(in)}})
		case "web_search_tool_result":
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": b})
		case "text":
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": b["text"]}})
		}
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	ev("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": len(answer) / 4, "server_tool_use": map[string]int{"web_search_requests": 1}}})
	ev("message_stop", map[string]any{"type": "message_stop"})
	flusher.Flush()
	return nil
}
