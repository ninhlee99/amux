package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

type fakeBackend struct {
	asked AskRequest
}

func (f *fakeBackend) Providers() ([]ProviderInfo, error) {
	return []ProviderInfo{{ID: "chatgpt:01", Type: "chatgpt_web", Tier: "web", InPool: true, Configured: true}}, nil
}

func (f *fakeBackend) Ask(ctx context.Context, r AskRequest, onDelta func(string)) (*AskResult, error) {
	f.asked = r
	if r.Prompt == "block" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.Provider == "broken" {
		return nil, errors.New("provider \"broken\" not addressable")
	}
	onDelta("partial")
	return &AskResult{Provider: "chatgpt:01", Text: "answer to " + r.Prompt}, nil
}

func (f *fakeBackend) Status(context.Context) (map[string]any, error) {
	return map[string]any{"gateway": map[string]any{"running": false}}, nil
}

// session drives a Server over in-memory pipes.
type session struct {
	t   *testing.T
	in  *io.PipeWriter
	out *bufio.Scanner
}

func startSession(t *testing.T, s *Server) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_ = s.Serve(context.Background(), inR, outW)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	return &session{t: t, in: inW, out: sc}
}

func (s *session) send(msg string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, msg+"\n"); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) next() map[string]any {
	s.t.Helper()
	done := make(chan bool, 1)
	var m map[string]any
	go func() {
		ok := s.out.Scan()
		if ok {
			_ = json.Unmarshal(s.out.Bytes(), &m)
		}
		done <- ok
	}()
	select {
	case ok := <-done:
		if !ok {
			s.t.Fatal("server closed output")
		}
	case <-time.After(5 * time.Second):
		s.t.Fatal("timed out waiting for server output")
	}
	return m
}

// result waits for the response with the given id, skipping notifications.
func (s *session) result(id float64) map[string]any {
	s.t.Helper()
	for {
		m := s.next()
		if m["id"] == id {
			return m
		}
	}
}

func newTestServer() (*Server, *fakeBackend) {
	s := NewServer("amux", "test")
	b := &fakeBackend{}
	RegisterAmuxTools(s, b)
	return s, b
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s, _ := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	res := sess.result(1)["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Fatalf("version = %v", res["protocolVersion"])
	}
	sess.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	sess.send(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := sess.result(2)["result"].(map[string]any)["protocolVersion"]; v != SupportedProtocolVersions[0] {
		t.Fatalf("unknown version should fall back to latest, got %v", v)
	}
}

func TestToolsListHasSchemas(t *testing.T) {
	s, _ := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := sess.result(1)["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tl := raw.(map[string]any)
		names[tl["name"].(string)] = true
		if tl["inputSchema"].(map[string]any)["type"] != "object" {
			t.Fatalf("%v: inputSchema must be an object schema", tl["name"])
		}
	}
	for _, want := range []string{"amux_ask", "amux_providers", "amux_status"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestToolsCallAskWithProgress(t *testing.T) {
	s, b := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"amux_ask","arguments":{"prompt":"why?","provider":"chatgpt"},"_meta":{"progressToken":"p1"}}}`)
	sawProgress := false
	var resp map[string]any
	for resp == nil {
		m := sess.next()
		if m["method"] == "notifications/progress" {
			sawProgress = m["params"].(map[string]any)["progressToken"] == "p1"
			continue
		}
		resp = m
	}
	res := resp["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("unexpected error: %v", res)
	}
	sc := res["structuredContent"].(map[string]any)
	if sc["text"] != "answer to why?" || sc["provider"] != "chatgpt:01" {
		t.Fatalf("structured = %v", sc)
	}
	if !sawProgress {
		t.Fatal("expected a progress notification")
	}
	if b.asked.Provider != "chatgpt" {
		t.Fatalf("provider not forwarded: %+v", b.asked)
	}
}

func TestToolErrorsAreResults(t *testing.T) {
	s, _ := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"amux_ask","arguments":{"prompt":"x","provider":"broken"}}}`)
	res := sess.result(1)["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "not addressable") {
		t.Fatalf("res = %v", res)
	}
	sess.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"amux_ask","arguments":{}}}`)
	if res := sess.result(2)["result"].(map[string]any); res["isError"] != true {
		t.Fatalf("missing prompt should be a tool error: %v", res)
	}
	sess.send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nope"}}`)
	if e := sess.result(3)["error"].(map[string]any); e["code"].(float64) != codeInvalidParams {
		t.Fatalf("error = %v", e)
	}
	sess.send(`{"jsonrpc":"2.0","id":4,"method":"does/not/exist"}`)
	if e := sess.result(4)["error"].(map[string]any); e["code"].(float64) != codeMethodNotFound {
		t.Fatalf("error = %v", e)
	}
	sess.send(`not json`)
	if e := sess.next()["error"].(map[string]any); e["code"].(float64) != codeParseError {
		t.Fatalf("error = %v", e)
	}
}

func TestCancellationStopsLongCall(t *testing.T) {
	s, _ := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"amux_ask","arguments":{"prompt":"block"}}}`)
	// ping is answered while the long call runs
	sess.send(`{"jsonrpc":"2.0","id":10,"method":"ping"}`)
	if m := sess.result(10); m["error"] != nil {
		t.Fatalf("ping failed: %v", m)
	}
	sess.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":9,"reason":"user"}}`)
	res := sess.result(9)["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("cancelled call should end with an error result: %v", res)
	}
}

func TestAmuxReviewAndContext(t *testing.T) {
	s, backend := newTestServer()
	sess := startSession(t, s)

	// Test amux_ask with context
	sess.send(`{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"amux_ask","arguments":{"prompt":"what is this?","context":"file: main.go"}}}`)
	res := sess.result(20)
	if res["error"] != nil {
		t.Fatalf("amux_ask context error: %v", res["error"])
	}
	if !strings.Contains(backend.asked.Prompt, "[Workspace Context]") || !strings.Contains(backend.asked.Prompt, "file: main.go") {
		t.Fatalf("expected context injected in prompt, got: %s", backend.asked.Prompt)
	}

	// Test amux_review
	sess.send(`{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"amux_review","arguments":{"diff":"+ func New() {}","focus":"security"}}}`)
	res2 := sess.result(21)
	if res2["error"] != nil {
		t.Fatalf("amux_review error: %v", res2["error"])
	}
	if !strings.Contains(backend.asked.Prompt, "+ func New() {}") {
		t.Fatalf("expected diff in prompt, got: %s", backend.asked.Prompt)
	}
	// Test amux_diagnose
	sess.send(`{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"amux_diagnose","arguments":{"error":"nil pointer dereference","code":"func a() { var x *int; *x = 1 }"}}}`)
	res3 := sess.result(22)
	if res3["error"] != nil {
		t.Fatalf("amux_diagnose error: %v", res3["error"])
	}
	if !strings.Contains(backend.asked.Prompt, "nil pointer dereference") {
		t.Fatalf("expected error in prompt, got: %s", backend.asked.Prompt)
	}

	// Test amux_fix
	sess.send(`{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"amux_fix","arguments":{"file_content":"func a() {}","issue":"add return int"}}}`)
	res4 := sess.result(23)
	if res4["error"] != nil {
		t.Fatalf("amux_fix error: %v", res4["error"])
	}
	if !strings.Contains(backend.asked.Prompt, "add return int") {
		t.Fatalf("expected issue in prompt, got: %s", backend.asked.Prompt)
	}

	// Test amux_analyze
	sess.send(`{"jsonrpc":"2.0","id":24,"method":"tools/call","params":{"name":"amux_analyze","arguments":{"structure":"pkg/a -> pkg/b","objective":"reduce coupling"}}}`)
	res5 := sess.result(24)
	if res5["error"] != nil {
		t.Fatalf("amux_analyze error: %v", res5["error"])
	}
	if !strings.Contains(backend.asked.Prompt, "reduce coupling") {
		t.Fatalf("expected objective in prompt, got: %s", backend.asked.Prompt)
	}
}

type namedAdapter struct{ id string }

func (n namedAdapter) ID() string    { return n.id }
func (n namedAdapter) Priority() int { return 1 }
func (n namedAdapter) SendMessageStream(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}

func TestPoolBackend_Providers_IdentitiesIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "identities.json")
	_ = os.WriteFile(idPath, []byte(`{
		"threshold_pct": 95,
		"identities": [
			{
				"id": "claude:code:01",
				"provider": "anthropic",
				"tier": "subscription",
				"auth_type": "oauth",
				"active": true
			}
		]
	}`), 0o600)

	t.Setenv("AMUX_HOME", tmpDir)
	backend := &PoolBackend{
		AccountsPath: filepath.Join(tmpDir, "accounts.json"),
	}
	providers, err := backend.Providers()
	if err != nil {
		t.Fatalf("expected backend.Providers() to succeed using identities.json, got: %v", err)
	}
	if len(providers) == 0 {
		t.Fatalf("expected at least 1 provider from identities.json, got 0")
	}
	if providers[0].ID != "claude:code:01" {
		t.Fatalf("expected provider ID 'claude:code:01', got %s", providers[0].ID)
	}
}


