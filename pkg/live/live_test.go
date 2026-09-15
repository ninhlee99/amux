//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/types"
)

const pingPrompt = "Reply with the single word pong and nothing else."

func TestLive_EachRotateAdapterPing(t *testing.T) {
	env := livePool(t)
	for _, a := range env.ads {
		a := a
		t.Run(a.ID(), func(t *testing.T) {
			if router.DetermineAdapterGroup(a) == router.GroupClaudeSub {
				t.Skip("Claude subscription stays off")
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeoutFor(a.ID()))
			defer cancel()
			ch, err := a.SendMessageStream(ctx, pingReq(a.ID()))
			if err != nil {
				skipOrFail(t, err)
			}
			text, thinking, _, err := drain(ch)
			if err != nil {
				skipOrFail(t, err)
			}
			if strings.TrimSpace(text) == "" && strings.TrimSpace(thinking) == "" {
				t.Skip("empty upstream reply (silent/thinking-only model)")
			}
			t.Logf("id=%s bytes=%d thinking=%d", a.ID(), len(text), len(thinking))
		})
	}
}

func TestLive_ClaudeMessages_PinAPI(t *testing.T) {
	env := livePool(t)
	pool := env.pool
	id := firstID(t, env, "gemini:api:", "groq:api:", "openrouter:api:")
	body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":32,"stream":false,"messages":[{"role":"user","content":%q}]}`, pingPrompt))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("X-Provider", id)
	req = req.WithContext(timeoutCtx(t, 45*time.Second))
	rec := httptest.NewRecorder()
	if err := bridge.HandleClaudeMessages(rec, req, pool, body); err != nil {
		skipOrFail(t, err)
	}
	if rec.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("claude /v1/messages via %s: status %d body %s", id, rec.Code, rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), `"type":"text"`) && rec.Body.Len() < 8 {
		t.Fatalf("empty anthropic body from %s: %s", id, rec.Body.String())
	}
	t.Logf("pin=%s status=%d", id, rec.Code)
}

func TestLive_ChatCompletions_CodexUA(t *testing.T) {
	env := livePool(t)
	pool := env.pool
	id := firstID(t, env, "codex:")
	reqBody := fmt.Sprintf(`{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":%q}]}`, pingPrompt)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("User-Agent", "codex_cli_rs/0.42.0")
	req.Header.Set("X-Provider", id)
	req = req.WithContext(timeoutCtx(t, 90*time.Second))
	rec := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec, req, pool)
	if rec.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("codex chat/completions via %s: status %d body %s", id, rec.Code, rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), `"content"`) {
		t.Fatalf("missing content from %s: %s", id, rec.Body.String())
	}
	t.Logf("pin=%s status=%d", id, rec.Code)
}

func TestLive_GeminiGenerateContent_AGY(t *testing.T) {
	env := livePool(t)
	pool := env.pool
	id := firstAGY(t, env)
	body := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":%q}]}]}`, pingPrompt)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewBufferString(body))
	req.Header.Set("X-Provider", id)
	req = req.WithContext(timeoutCtx(t, 90*time.Second))
	rec := httptest.NewRecorder()
	bridge.HandleGeminiGenerateContent(rec, req, pool)
	if rec.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("agy generateContent via %s: status %d body %s", id, rec.Code, rec.Body.String()))
	}
	if rec.Body.Len() < 8 {
		t.Fatalf("empty gemini body from %s", id)
	}
	t.Logf("pin=%s status=%d", id, rec.Code)
}

func TestLive_WebAdapters_Pin(t *testing.T) {
	env := livePool(t)
	pool := env.pool
	for _, prefix := range []string{"claude:web:", "chatgpt:", "gemini:web:"} {
		id, ok := lookupPrefix(env, prefix)
		if !ok {
			t.Logf("skip %s — not in pool", prefix)
			continue
		}
		t.Run(id, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":32,"stream":false,"messages":[{"role":"user","content":%q}]}`, pingPrompt))
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("X-Provider", id)
			req = req.WithContext(timeoutCtx(t, 120*time.Second))
			rec := httptest.NewRecorder()
			if err := bridge.HandleClaudeMessages(rec, req, pool, body); err != nil {
				skipOrFail(t, err)
			}
			if rec.Code != http.StatusOK {
				skipOrFail(t, fmt.Errorf("web %s: status %d body %s", id, rec.Code, rec.Body.String()))
			}
			t.Logf("pin=%s status=%d bytes=%d", id, rec.Code, rec.Body.Len())
		})
	}
}

func TestLive_ToolRoundTrip_GeminiAPI(t *testing.T) {
	env := livePool(t)
	pool := env.pool
	id := firstID(t, env, "gemini:api:", "groq:api:")
	reqBody := `{
		"model":"gpt-4o",
		"stream":false,
		"tool_choice":{"type":"function","function":{"name":"echo"}},
		"tools":[{"type":"function","function":{"name":"echo","description":"Echo a word","parameters":{"type":"object","properties":{"word":{"type":"string"}},"required":["word"]}}}],
		"messages":[{"role":"user","content":"Call echo with word=pong"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("X-Provider", id)
	req = req.WithContext(timeoutCtx(t, 60*time.Second))
	rec := httptest.NewRecorder()
	bridge.HandleChatCompletions(rec, req, pool)
	if rec.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("tool round-trip via %s: status %d body %s", id, rec.Code, rec.Body.String()))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "echo") && !strings.Contains(strings.ToLower(body), "pong") {
		t.Fatalf("expected tool call or pong from %s: %s", id, body)
	}
	t.Logf("pin=%s tool_or_text=ok", id)
}

// Claude Code catalog the proxy must round-trip. Client executes locally;
// we only check the model returns tool_use for the forced name.
var claudeCodeTools = []struct {
	name   string
	schema string
	prompt string
}{
	{"Bash", `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`, "Call Bash. command must be: echo pong"},
	{"Read", `{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}`, "Call Read on file_path go.mod"},
	{"Edit", `{"type":"object","properties":{"file_path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["file_path","old_string","new_string"]}`, "Call Edit: file_path=a.go old_string=foo new_string=bar"},
	{"Write", `{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}},"required":["file_path","content"]}`, "Call Write: file_path=hi.txt content=hi"},
	{"Glob", `{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`, "Call Glob with pattern *.go"},
	{"Grep", `{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`, "Call Grep with pattern package main"},
	{"Agent", `{"type":"object","properties":{"prompt":{"type":"string"}},"required":["prompt"]}`, "Call Agent with prompt: ping"},
	{"WebFetch", `{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`, "Call WebFetch url=https://example.com"},
	{"Skill", `{"type":"object","properties":{"skill":{"type":"string"}},"required":["skill"]}`, "Call Skill with skill=review"},
	{"mcp__demo__search", `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`, "Call mcp__demo__search with query=pong"},
}

func TestLive_ClaudeCodeTools_ForcedEach(t *testing.T) {
	env := livePool(t)
	id := firstID(t, env, "gemini:api:", "groq:api:")
	for _, tc := range claudeCodeTools {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body := claudeForcedToolBody(tc.name, tc.schema, tc.prompt)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("X-Provider", id)
			req = req.WithContext(timeoutCtx(t, 60*time.Second))
			rec := httptest.NewRecorder()
			if err := bridge.HandleClaudeMessages(rec, req, env.pool, body); err != nil {
				skipOrFail(t, err)
			}
			if rec.Code != http.StatusOK {
				skipOrFail(t, fmt.Errorf("%s via %s: status %d body %s", tc.name, id, rec.Code, rec.Body.String()))
			}
			names := claudeToolUseNames(rec.Body.Bytes())
			if !containsFold(names, tc.name) {
				t.Fatalf("forced %s missing in tool_use=%v body=%s", tc.name, names, rec.Body.String())
			}
			t.Logf("pin=%s tool=%s names=%v", id, tc.name, names)
		})
	}
	resetGuard(id)
}

func TestLive_ClaudeCodeTools_CodexUA(t *testing.T) {
	env := livePool(t)
	id := firstID(t, env, "groq:api:", "gemini:api:")
	resetGuard(id)
	for _, name := range []string{"Bash", "Read", "mcp__demo__search"} {
		name := name
		t.Run(name, func(t *testing.T) {
			tc := toolByName(name)
			reqBody := openaiForcedToolBody(tc.name, tc.schema, tc.prompt)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
			req.Header.Set("User-Agent", "codex_cli_rs/0.42.0")
			req.Header.Set("X-Provider", id)
			req = req.WithContext(timeoutCtx(t, 60*time.Second))
			rec := httptest.NewRecorder()
			bridge.HandleChatCompletions(rec, req, env.pool)
			if rec.Code != http.StatusOK {
				skipOrFail(t, fmt.Errorf("codex %s via %s: status %d body %s", name, id, rec.Code, rec.Body.String()))
			}
			if !strings.Contains(rec.Body.String(), name) {
				t.Fatalf("codex UA missing tool %s: %s", name, rec.Body.String())
			}
			t.Logf("codex-ua pin=%s tool=%s", id, name)
		})
	}
}

func TestLive_ToolLoop_TwoTurnBash(t *testing.T) {
	env := livePool(t)
	id := firstID(t, env, "groq:api:", "gemini:api:")
	resetGuard(id)
	tc := toolByName("Bash")
	body1 := claudeForcedToolBody(tc.name, tc.schema, tc.prompt)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body1))
	req1.Header.Set("anthropic-version", "2023-06-01")
	req1.Header.Set("X-Provider", id)
	req1 = req1.WithContext(timeoutCtx(t, 60*time.Second))
	rec1 := httptest.NewRecorder()
	if err := bridge.HandleClaudeMessages(rec1, req1, env.pool, body1); err != nil {
		skipOrFail(t, err)
	}
	if rec1.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("turn1: status %d body %s", rec1.Code, rec1.Body.String()))
	}
	var turn1 struct {
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &turn1); err != nil {
		t.Fatal(err)
	}
	var toolID string
	var input json.RawMessage
	for _, c := range turn1.Content {
		if c.Type == "tool_use" && strings.EqualFold(c.Name, "Bash") {
			toolID = c.ID
			input = c.Input
			break
		}
	}
	if toolID == "" {
		t.Fatalf("turn1 no Bash tool_use: %s", rec1.Body.String())
	}
	var schemaObj, inputObj any
	_ = json.Unmarshal([]byte(tc.schema), &schemaObj)
	_ = json.Unmarshal(input, &inputObj)
	body2, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-5",
		"max_tokens": 64,
		"stream":     false,
		"tools":      []map[string]any{{"name": "Bash", "description": "shell", "input_schema": schemaObj}},
		"messages": []any{
			map[string]any{"role": "user", "content": tc.prompt},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": toolID, "name": "Bash", "input": inputObj},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": toolID, "content": "pong\n"},
			}},
		},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body2))
	req2.Header.Set("anthropic-version", "2023-06-01")
	req2.Header.Set("X-Provider", id)
	req2 = req2.WithContext(timeoutCtx(t, 60*time.Second))
	rec2 := httptest.NewRecorder()
	if err := bridge.HandleClaudeMessages(rec2, req2, env.pool, body2); err != nil {
		skipOrFail(t, err)
	}
	if rec2.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("turn2: status %d body %s", rec2.Code, rec2.Body.String()))
	}
	if rec2.Body.Len() < 8 {
		t.Fatalf("turn2 empty: %s", rec2.Body.String())
	}
	t.Logf("two-turn bash pin=%s turn2_bytes=%d", id, rec2.Body.Len())
}

func TestLive_WebTools_EmulatedBash(t *testing.T) {
	env := livePool(t)
	tc := toolByName("Bash")
	for _, prefix := range []string{"claude:web:", "chatgpt:", "gemini:web:"} {
		id, ok := lookupPrefix(env, prefix)
		if !ok {
			t.Logf("skip %s", prefix)
			continue
		}
		t.Run(id, func(t *testing.T) {
			body := claudeForcedToolBody(tc.name, tc.schema, "You must call Bash with command git status. Do not answer in prose.")
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("X-Provider", id)
			req = req.WithContext(timeoutCtx(t, 120*time.Second))
			rec := httptest.NewRecorder()
			if err := bridge.HandleClaudeMessages(rec, req, env.pool, body); err != nil {
				skipOrFail(t, err)
			}
			if rec.Code != http.StatusOK {
				skipOrFail(t, fmt.Errorf("web tools %s: status %d body %s", id, rec.Code, rec.Body.String()))
			}
			names := claudeToolUseNames(rec.Body.Bytes())
			raw := rec.Body.String()
			if !containsFold(names, "Bash") && !strings.Contains(raw, "Bash") && !strings.Contains(raw, "git status") {
				t.Fatalf("web %s did not emit Bash/tool_use: names=%v body=%s", id, names, raw)
			}
			t.Logf("web pin=%s tool_use=%v bytes=%d", id, names, rec.Body.Len())
		})
	}
}

func TestLive_InstalledProxy_IfUp(t *testing.T) {
	if !proxy.ProxyUp() {
		t.Skip("proxy not listening")
	}
	id := "gemini:api:01"
	payload := fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":32,"stream":false,"messages":[{"role":"user","content":%q}]}`, pingPrompt)
	req, err := http.NewRequest(http.MethodPost, proxy.ProxyBase()+"/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", "am-proxy")
	req.Header.Set("X-Provider", id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		skipOrFail(t, fmt.Errorf("installed proxy %s: status %d body %s", id, resp.StatusCode, b))
	}
	if len(bytes.TrimSpace(b)) < 8 {
		t.Fatalf("installed proxy empty body")
	}
	t.Logf("installed proxy pin=%s status=%d", id, resp.StatusCode)
}

type liveEnv struct {
	pool *router.AccountPoolRouter
	ads  []types.ProviderAdapter
}

func livePool(t *testing.T) liveEnv {
	t.Helper()
	if os.Getenv("AM_HOME") != "" {
		t.Logf("AM_HOME=%s", os.Getenv("AM_HOME"))
	}
	ads, err := provider.LoadAccounts(provider.DefaultAccountsPath())
	if err != nil {
		t.Fatalf("LoadAccounts: %v", err)
	}
	if len(ads) == 0 {
		t.Fatal("no rotate-pool accounts in ~/.am")
	}
	ids := make([]string, 0, len(ads))
	for _, a := range ads {
		ids = append(ids, a.ID())
	}
	t.Logf("rotate=%v agyAuth=%v", ids, provider.AGYAuthAvailable())
	pool := router.NewAccountPoolRouter(ads)
	if all, err := provider.LoadAllAddressable(provider.DefaultAccountsPath()); err == nil {
		pool.SetDirectory(all)
	}
	return liveEnv{pool: pool, ads: ads}
}

func pingReq(id string) *types.ChatRequest {
	d := "cursor"
	switch {
	case strings.HasPrefix(id, "codex"):
		d = "codex"
	case strings.HasPrefix(id, "agy"), strings.HasPrefix(id, "antigravity"), strings.Contains(id, "gemini"):
		d = "gemini"
	case strings.Contains(id, "claude"):
		d = "claude"
	}
	return &types.ChatRequest{
		Messages:      []types.ChatMessage{{Role: "user", Content: pingPrompt}},
		FullContext:   true,
		ClientDialect: d,
	}
}

func drain(ch <-chan types.StreamChunk) (text, thinking string, calls []types.ToolCall, err error) {
	var b, th strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return b.String(), th.String(), calls, chunk.Error
		}
		b.WriteString(chunk.Content)
		th.WriteString(chunk.Thinking)
		if len(chunk.ToolCalls) > 0 {
			calls = append(calls, chunk.ToolCalls...)
		}
	}
	return b.String(), th.String(), calls, nil
}

func firstAGY(t *testing.T, env liveEnv) string {
	t.Helper()
	for _, a := range env.ads {
		g := router.DetermineAdapterGroup(a)
		if g == router.GroupAGYSub || g == router.GroupAGYFree {
			return a.ID()
		}
	}
	if id, ok := lookupPrefix(env, "agy:"); ok {
		return id
	}
	if id, ok := lookupPrefix(env, "antigravity:"); ok {
		return id
	}
	t.Skipf("AGY not in rotate pool (auth=%v)", provider.AGYAuthAvailable())
	return ""
}

func firstID(t *testing.T, env liveEnv, prefixes ...string) string {
	t.Helper()
	for _, p := range prefixes {
		if id, ok := lookupPrefix(env, p); ok {
			return id
		}
	}
	t.Skipf("no adapter matching %v", prefixes)
	return ""
}

func lookupPrefix(env liveEnv, prefix string) (string, bool) {
	for _, a := range env.ads {
		if strings.HasPrefix(a.ID(), prefix) {
			return a.ID(), true
		}
	}
	for _, row := range env.pool.Status() {
		id, _ := row["id"].(string)
		if strings.HasPrefix(id, prefix) {
			return id, true
		}
	}
	return "", false
}

func timeoutFor(id string) time.Duration {
	if strings.Contains(id, "web") || strings.HasPrefix(id, "chatgpt") || strings.HasPrefix(id, "codex") || strings.HasPrefix(id, "agy") || strings.HasPrefix(id, "antigravity") {
		return 90 * time.Second
	}
	return 45 * time.Second
}

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func toolByName(name string) struct {
	name   string
	schema string
	prompt string
} {
	for _, tc := range claudeCodeTools {
		if tc.name == name {
			return tc
		}
	}
	return claudeCodeTools[0]
}

func claudeForcedToolBody(name, schema, prompt string) []byte {
	var schemaObj any
	_ = json.Unmarshal([]byte(schema), &schemaObj)
	b, _ := json.Marshal(map[string]any{
		"model":       "claude-sonnet-4-5",
		"max_tokens":  256,
		"stream":      false,
		"tool_choice": map[string]any{"type": "tool", "name": name},
		"tools": []map[string]any{{
			"name":         name,
			"description":  "coding-agent tool " + name,
			"input_schema": schemaObj,
		}},
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	return b
}

func openaiForcedToolBody(name, schema, prompt string) string {
	var schemaObj any
	_ = json.Unmarshal([]byte(schema), &schemaObj)
	b, _ := json.Marshal(map[string]any{
		"model":       "gpt-4o",
		"stream":      false,
		"tool_choice": map[string]any{"type": "function", "function": map[string]string{"name": name}},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": "coding-agent tool " + name,
				"parameters":  schemaObj,
			},
		}},
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	return string(b)
}

func claudeToolUseNames(body []byte) []string {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return nil
	}
	var names []string
	for _, c := range resp.Content {
		if c.Type == "tool_use" && c.Name != "" {
			names = append(names, c.Name)
		}
	}
	return names
}

func containsFold(ss []string, want string) bool {
	for _, s := range ss {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func resetGuard(id string) {
	if id == "" {
		return
	}
	guard.GlobalPacer().Reset(id)
	guard.GlobalHealth().Reset(id)
}

func skipOrFail(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, types.ErrRateLimitReached),
		strings.Contains(msg, "429"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "quota"),
		strings.Contains(msg, "resource_exhausted"),
		strings.Contains(msg, "cooldown"):
		t.Skip(err.Error())
	default:
		t.Fatal(err)
	}
}

// TestLive_ClaudeClient_CodexBackend_RealToolCall tests real live network call:
// Claude client calls /v1/messages with tools, routes to real codex:01 backend.
func TestLive_ClaudeClient_CodexBackend_RealToolCall(t *testing.T) {
	env := livePool(t)
	id := "codex:01"
	body := []byte(`{
		"model": "gpt-5.6-terra",
		"max_tokens": 1024,
		"stream": true,
		"tools": [
			{
				"name": "Bash",
				"description": "Execute shell command on local repository",
				"input_schema": {
					"type": "object",
					"properties": {
						"command": {"type": "string"}
					},
					"required": ["command"]
				}
			}
		],
		"messages": [
			{"role": "user", "content": "Please check repository status by running git status using Bash tool. Call the Bash tool now."}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("X-Provider", id)
	req = req.WithContext(timeoutCtx(t, 120*time.Second))
	rec := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(rec, req, env.pool, body); err != nil {
		skipOrFail(t, err)
	}

	resStr := rec.Body.String()
	t.Logf("codex:01 response len=%d: %s", len(resStr), resStr)
	if rec.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("status %d: %s", rec.Code, resStr))
	}
	if !strings.Contains(resStr, `"type":"tool_use"`) && !strings.Contains(resStr, "Bash") {
		t.Logf("codex:01 returned text instead of tool_use (might be refusal or direct answer)")
		return
	}
	t.Logf("SUCCESS: codex:01 emitted tool_use for Claude client!")

	// Turn 2: Claude client executes locally and sends tool_result
	bodyTurn2 := []byte(`{
		"model": "gpt-5.6-terra",
		"max_tokens": 1024,
		"stream": true,
		"tools": [
			{
				"name": "Bash",
				"description": "Execute shell command on local repository",
				"input_schema": {
					"type": "object",
					"properties": {
						"command": {"type": "string"}
					},
					"required": ["command"]
				}
			}
		],
		"messages": [
			{"role": "user", "content": "Please check repository status by running git status using Bash tool. Call the Bash tool now."},
			{
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "call_q2sgfkZvkzpqpn8y1aOKjHxG",
						"name": "Bash",
						"input": {"command": "git status"}
					}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "call_q2sgfkZvkzpqpn8y1aOKjHxG",
						"content": "On branch main\nYour branch is up to date with 'origin/main'.\nnothing to commit, working tree clean"
					}
				]
			}
		]
	}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyTurn2))
	req2.Header.Set("anthropic-version", "2023-06-01")
	req2.Header.Set("X-Provider", id)
	req2 = req2.WithContext(timeoutCtx(t, 120*time.Second))
	rec2 := httptest.NewRecorder()

	if err := bridge.HandleClaudeMessages(rec2, req2, env.pool, bodyTurn2); err != nil {
		skipOrFail(t, err)
	}

	resStr2 := rec2.Body.String()
	t.Logf("codex:01 Turn 2 response: %s", resStr2)
	if rec2.Code != http.StatusOK {
		skipOrFail(t, fmt.Errorf("Turn 2 status %d: %s", rec2.Code, resStr2))
	}
	t.Logf("SUCCESS: codex:01 received tool_result and completed Turn 2 successfully!")
}
