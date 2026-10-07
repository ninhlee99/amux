package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/types"
)

// liveTimeout is per check: web accounts often take 20–40s for one turn.
const liveTimeout = 90 * time.Second

// liveResult is one end-to-end check through the running gateway.
type liveResult struct {
	Name   string
	OK     bool
	Detail string
	ReqID  string
	Took   time.Duration
	Fix    []string
}

// CmdDoctorLive sends real turns through the local gateway — the same path an
// IDE takes — and names the first layer that fails. It spends a few tokens on
// whichever account the router (or --provider) picks.
func CmdDoctorLive(args []string) {
	pin := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--provider" && i+1 < len(args):
			pin = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--provider="):
			pin = strings.TrimPrefix(args[i], "--provider=")
		}
	}
	fmt.Println("== AMUX Live Check (real requests through the gateway) ==")
	if pin != "" {
		fmt.Printf("Pinned to: %s\n", pin)
	}
	results := runLiveChecks(proxy.ProxyBase(), pin, &http.Client{Timeout: liveTimeout})
	failed := 0
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
			failed++
		}
		line := fmt.Sprintf("%s %-22s %s", mark, r.Name, r.Detail)
		if r.Took > 0 {
			line += fmt.Sprintf(" (%s)", r.Took.Round(10*time.Millisecond))
		}
		if r.ReqID != "" {
			line += "  req=" + r.ReqID
		}
		fmt.Println(line)
		for _, f := range r.Fix {
			fmt.Println("    → " + f)
		}
	}
	if failed == 0 {
		fmt.Println("\nAll live checks passed.")
		return
	}
	fmt.Printf("\n%d check(s) failed. Look up a req=… id in ~/.amux/gateway.log and ~/.amux/errors.log.\n", failed)
}

// runLiveChecks stops after the gateway probe fails, since nothing else can pass.
func runLiveChecks(base, pin string, c *http.Client) []liveResult {
	gw := checkGatewayUp(base, c)
	if !gw.OK {
		return []liveResult{gw}
	}
	return []liveResult{
		gw,
		checkAnthropicTurn(base, pin, c),
		checkOpenAIStream(base, pin, c),
		checkToolRoundtrip(base, pin, c),
	}
}

func checkGatewayUp(base string, c *http.Client) liveResult {
	r := liveResult{Name: "Gateway reachable"}
	start := time.Now()
	resp, err := c.Get(base + "/_am/status")
	r.Took = time.Since(start)
	if err != nil {
		r.Detail = "no answer at " + base
		r.Fix = []string{"amux start", "amux status"}
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.Detail = fmt.Sprintf("HTTP %d from %s/_am/status", resp.StatusCode, base)
		r.Fix = []string{"amux restart"}
		return r
	}
	r.OK = true
	r.Detail = base
	return r
}

func livePost(base, path, pin string, body any, c *http.Client) (*http.Response, string, time.Duration, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(b))
	if err != nil {
		return nil, "", 0, err
	}
	reqID := types.NewRequestID()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "am-proxy") // local placeholder, never an upstream key
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set(types.RequestIDHeader, reqID)
	if pin != "" {
		req.Header.Set("X-Provider", pin)
	}
	start := time.Now()
	resp, err := c.Do(req)
	if resp != nil {
		if id := resp.Header.Get(types.RequestIDHeader); id != "" {
			reqID = id
		}
	}
	return resp, reqID, time.Since(start), err
}

// failureFixes maps a failed turn to the commands most likely to repair it.
func failureFixes(status int, msg string) []string {
	lower := strings.ToLower(msg)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden ||
		strings.Contains(lower, "auth") || strings.Contains(lower, "expired") || strings.Contains(lower, "login"):
		return []string{"amux doctor providers   # find the account whose session expired", "amux login <provider>   # sign in again"}
	case status == http.StatusTooManyRequests || strings.Contains(lower, "rate") || strings.Contains(lower, "quota") ||
		strings.Contains(lower, "cooling") || strings.Contains(lower, "quarantin"):
		return []string{"amux status             # see cooldown / quarantine timers", "amux pool add <id>      # add another account to rotation"}
	case strings.Contains(lower, "no active accounts") || strings.Contains(lower, "no adapters"):
		return []string{"amux login <provider>", "amux pool add <id>"}
	default:
		return []string{"amux doctor providers   # probe each account directly, bypassing the gateway"}
	}
}

func errorMessage(body []byte) string {
	var e struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != nil {
		switch v := e.Error.(type) {
		case string:
			return v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				return m
			}
		}
	}
	return strings.TrimSpace(string(body))
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// anthropicReply is the subset of an Anthropic Messages reply the checks read.
type anthropicReply struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
}

func postAnthropic(name, base, pin string, body map[string]any, c *http.Client) (liveResult, *anthropicReply) {
	r := liveResult{Name: name}
	resp, reqID, took, err := livePost(base, "/v1/messages", pin, body, c)
	r.ReqID, r.Took = reqID, took
	if err != nil {
		r.Detail = "request failed: " + err.Error()
		r.Fix = failureFixes(0, err.Error())
		return r, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := errorMessage(raw)
		r.Detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, clip(msg, 160))
		r.Fix = failureFixes(resp.StatusCode, msg)
		return r, nil
	}
	var reply anthropicReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		r.Detail = "reply is not Anthropic JSON: " + clip(string(raw), 120)
		r.Fix = []string{"amux restart   # bridge returned an unexpected body"}
		return r, nil
	}
	return r, &reply
}

func checkAnthropicTurn(base, pin string, c *http.Client) liveResult {
	r, reply := postAnthropic("Anthropic dialect", base, pin, map[string]any{
		"model":      "default",
		"max_tokens": 64,
		"messages":   []map[string]any{{"role": "user", "content": "Reply with exactly: OK"}},
	}, c)
	if reply == nil {
		return r
	}
	var text strings.Builder
	for _, b := range reply.Content {
		text.WriteString(b.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		r.Detail = "empty reply (stop=" + reply.StopReason + ")"
		r.Fix = []string{"amux doctor providers   # the serving account answered with no text"}
		return r
	}
	r.OK = true
	r.Detail = fmt.Sprintf("%q", clip(text.String(), 40))
	return r
}

func checkOpenAIStream(base, pin string, c *http.Client) liveResult {
	r := liveResult{Name: "OpenAI dialect (SSE)"}
	resp, reqID, _, err := livePost(base, "/v1/chat/completions", pin, map[string]any{
		"model":    "default",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "Reply with exactly: OK"}},
	}, c)
	r.ReqID = reqID
	start := time.Now()
	if err != nil {
		r.Detail = "request failed: " + err.Error()
		r.Fix = failureFixes(0, err.Error())
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		msg := errorMessage(raw)
		r.Detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, clip(msg, 160))
		r.Fix = failureFixes(resp.StatusCode, msg)
		return r
	}
	var text strings.Builder
	var firstChunk time.Duration
	done := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk struct {
			Error   any `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Error != nil {
			msg := errorMessage([]byte(data))
			r.Detail = "stream error: " + clip(msg, 160)
			r.Fix = failureFixes(0, msg)
			return r
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" && firstChunk == 0 {
				firstChunk = time.Since(start)
			}
			text.WriteString(ch.Delta.Content)
		}
	}
	r.Took = time.Since(start)
	switch {
	case !done:
		r.Detail = "stream ended without [DONE] (cut off mid-reply)"
		r.Fix = []string{"amux restart", "amux doctor providers"}
	case strings.TrimSpace(text.String()) == "":
		r.Detail = "stream finished with no text"
		r.Fix = []string{"amux doctor providers   # the serving account answered with no text"}
	default:
		r.OK = true
		r.Detail = fmt.Sprintf("%q, first token after %s", clip(text.String(), 40), firstChunk.Round(10*time.Millisecond))
	}
	return r
}

func checkToolRoundtrip(base, pin string, c *http.Client) liveResult {
	r, reply := postAnthropic("Tool call roundtrip", base, pin, map[string]any{
		"model":      "default",
		"max_tokens": 256,
		"tools": []map[string]any{{
			"name":        "Bash",
			"description": "Run a shell command",
			"input_schema": map[string]any{
				"type":       "object",
				"required":   []string{"command"},
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
			},
		}},
		"messages": []map[string]any{{"role": "user", "content": "Use the Bash tool to run: echo amux-live-ok. Call the tool; do not answer in prose."}},
	}, c)
	if reply == nil {
		return r
	}
	for _, b := range reply.Content {
		if b.Type != "tool_use" {
			continue
		}
		var in map[string]any
		if json.Unmarshal(b.Input, &in) != nil {
			r.Detail = fmt.Sprintf("%s input is not a JSON object: %s", b.Name, clip(string(b.Input), 80))
			r.Fix = []string{"amux feedback   # tool parser produced an input the IDE will reject"}
			return r
		}
		cmd, _ := in["command"].(string)
		r.OK = true
		r.Detail = fmt.Sprintf("%s %q", b.Name, clip(cmd, 40))
		return r
	}
	var text strings.Builder
	for _, b := range reply.Content {
		text.WriteString(b.Text)
	}
	r.Detail = fmt.Sprintf("no tool_use (stop=%s, text %q)", reply.StopReason, clip(text.String(), 60))
	r.Fix = []string{
		"amux doctor --live --provider <id>   # try one account at a time to find the one that answers in prose",
		"amux status                          # text-only web accounts cannot drive IDE tools",
	}
	return r
}
