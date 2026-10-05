package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"amux-accounts/pkg/muse"
	"amux-accounts/pkg/types"
)

type fakeBackend struct {
	asked AskRequest
}

func (f *fakeBackend) Providers() ([]ProviderInfo, error) {
	return []ProviderInfo{{ID: "muse:web:01", Type: "muse_web", Tier: "web", InPool: true, Configured: true}}, nil
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

type fakeMuse struct{ prompt string }

func (m *fakeMuse) Status(context.Context) muse.Status { return muse.Status{LoggedIn: true} }
func (m *fakeMuse) Login(context.Context, time.Duration) (muse.AuthState, error) {
	return muse.AuthState{OK: true, ViewerID: "v"}, nil
}
func (m *fakeMuse) NewChat(context.Context) (string, error)          { return "https://muse.ai/thread/n", nil }
func (m *fakeMuse) OpenChat(context.Context, string) (string, error) { return "https://muse.ai/", nil }
func (m *fakeMuse) Chat(_ context.Context, p string, o muse.ChatOptions) (*muse.ChatResult, error) {
	m.prompt = p
	if o.OnDelta != nil {
		o.OnDelta("hi")
	}
	return &muse.ChatResult{Reply: "hi there", ThreadURL: "https://muse.ai/thread/t"}, nil
}
func (m *fakeMuse) ReadChat(context.Context, string, int) ([]muse.Message, string, error) {
	return []muse.Message{{Role: "user", Text: "q"}, {Role: "assistant", Text: "a", Media: []string{"https://muse.ai/files/x.png"}}}, "u", nil
}
func (m *fakeMuse) ListChats(context.Context, string) ([]muse.ChatInfo, error) {
	return []muse.ChatInfo{{Title: "Main chat"}}, nil
}
func (m *fakeMuse) Media(context.Context, string, bool, string) (*muse.MediaResult, error) {
	return &muse.MediaResult{URLs: []string{"https://muse.ai/files/x.png"}}, nil
}
func (m *fakeMuse) DumpDOM(context.Context, int) (*muse.DOMDump, error) { return &muse.DOMDump{}, nil }
func (m *fakeMuse) Close()                                              {}

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

func newTestServer() (*Server, *fakeBackend, *fakeMuse) {
	s := NewServer("amux", "test")
	b := &fakeBackend{}
	fm := &fakeMuse{}
	RegisterAmuxTools(s, b)
	RegisterMuseTools(s, func() MuseClient { return fm })
	return s, b, fm
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s, _, _ := newTestServer()
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
	s, _, _ := newTestServer()
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
	for _, want := range []string{"amux_ask", "amux_providers", "amux_status", "muse_chat", "muse_status", "muse_media", "muse_login"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestToolsCallAskWithProgress(t *testing.T) {
	s, b, _ := newTestServer()
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
	s, _, _ := newTestServer()
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
	s, _, _ := newTestServer()
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

func TestMuseChatTool(t *testing.T) {
	s, _, fm := newTestServer()
	sess := startSession(t, s)
	sess.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"muse_chat","arguments":{"prompt":"hello muse","new_thread":true}}}`)
	res := sess.result(1)["result"].(map[string]any)
	if res["structuredContent"].(map[string]any)["reply"] != "hi there" || fm.prompt != "hello muse" {
		t.Fatalf("res = %v", res)
	}
	sess.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"muse_read_last","arguments":{}}}`)
	if r := sess.result(2)["result"].(map[string]any)["structuredContent"].(map[string]any); r["reply"] != "a" {
		t.Fatalf("read_last = %v", r)
	}
}

type namedAdapter struct{ id string }

func (n namedAdapter) ID() string    { return n.id }
func (n namedAdapter) Priority() int { return 1 }
func (n namedAdapter) SendMessageStream(context.Context, *types.ChatRequest) (<-chan types.StreamChunk, error) {
	return nil, nil
}
