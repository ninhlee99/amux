package muse

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePage simulates the Muse web app at the level of the driver's scripts.
type fakePage struct {
	mu         sync.Mutex
	url        string
	assistants []string
	composer   string
	steps      []string // successive renders of the reply being generated
	step       int
	generating bool
	errNotice  string
	loggedIn   bool
	files      []string
	navigated  []string
	enterCount int
	closed     bool
}

func (f *fakePage) Eval(_ context.Context, expr string, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var v any
	switch {
	case expr == "location.href":
		v = f.url
	case expr == "document.readyState":
		v = "complete"
	case strings.Contains(expr, "'missing'"):
		v = "ready"
	case strings.Contains(expr, "/api/auth/check"):
		v = map[string]any{"ok": f.loggedIn, "status": 200, "viewerId": "viewer-1"}
	case strings.Contains(expr, "document.execCommand('delete')"):
		f.composer = ""
		v = true
	case strings.Contains(expr, "composer: composer"):
		if f.generating {
			if f.step < len(f.steps) {
				if len(f.assistants) == 0 || f.step == 0 {
					f.assistants = append(f.assistants, "")
				}
				f.assistants[len(f.assistants)-1] = f.steps[f.step]
				f.step++
			} else {
				f.generating = false
			}
		}
		last := ""
		if n := len(f.assistants); n > 0 {
			last = f.assistants[n-1]
		}
		v = map[string]any{"n": len(f.assistants), "last": last, "stop": f.generating,
			"err": f.errNotice, "approval": false, "composer": f.composer}
	case strings.Contains(expr, "filter(Boolean)"):
		var start int
		if i := strings.Index(expr, ".slice("); i >= 0 {
			_, _ = sscanInt(expr[i+len(".slice("):], &start)
		}
		var items []string
		for _, a := range f.assistants[min(start, len(f.assistants)):] {
			if s := strings.TrimSpace(a); s != "" {
				items = append(items, s)
			}
		}
		v = items
	case strings.HasPrefix(expr, "!!document.querySelector("):
		v = false
	case strings.Contains(expr, "new side chat"):
		f.url = "https://muse.ai/thread/11111111-2222-3333-4444-555555555555"
		v = true
	default:
		v = nil
	}
	if out == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}

func sscanInt(s string, n *int) (int, error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		*n = *n*10 + int(s[i]-'0')
		i++
	}
	return i, nil
}

func (f *fakePage) Navigate(_ context.Context, u string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.url = u
	f.navigated = append(f.navigated, u)
	return nil
}

func (f *fakePage) InsertText(_ context.Context, t string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.composer += t
	return nil
}

func (f *fakePage) PressKey(_ context.Context, key string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if key == "Enter" && strings.TrimSpace(f.composer) != "" {
		f.enterCount++
		f.composer = ""
		f.generating = true
		f.step = 0
	}
	return nil
}

func (f *fakePage) SetFileInputFiles(_ context.Context, _ string, paths []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = append(f.files, paths...)
	return nil
}

func (f *fakePage) Closed() bool { return f.closed }
func (f *fakePage) Close(bool)   { f.closed = true }

func newTestDriver(p *fakePage) *Driver {
	d := NewWithPage(Config{QuietPeriod: time.Millisecond, DoneIdle: 5 * time.Millisecond,
		NoStopIdle: 10 * time.Millisecond, Poll: time.Millisecond}, p)
	d.sleep = func(time.Duration) { time.Sleep(time.Millisecond) }
	return d
}

func TestChat_StreamsMonotonicTextAndReturnsReply(t *testing.T) {
	p := &fakePage{url: "https://muse.ai/", loggedIn: true,
		steps: []string{"Hel", "Hello", "Hello wor", "Hello world"}}
	d := newTestDriver(p)

	var deltas []string
	res, err := d.Chat(context.Background(), "Say hello", ChatOptions{Timeout: 5 * time.Second,
		OnDelta: func(s string) { deltas = append(deltas, s) }})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Reply != "Hello world" {
		t.Fatalf("reply = %q", res.Reply)
	}
	if p.enterCount != 1 {
		t.Fatalf("Enter pressed %d times", p.enterCount)
	}
	if len(deltas) == 0 || deltas[len(deltas)-1] != "Hello world" {
		t.Fatalf("deltas = %v", deltas)
	}
	for i := 1; i < len(deltas); i++ {
		if !strings.HasPrefix(deltas[i], deltas[i-1]) {
			t.Fatalf("non-monotonic deltas: %v", deltas)
		}
	}
}

func TestChat_RewrittenDraftIsNeverStreamed(t *testing.T) {
	// Muse renders a skeleton then rewrites it; the rewrite must not be
	// appended to what the client already received.
	p := &fakePage{url: "https://muse.ai/", loggedIn: true,
		steps: []string{"Thinking", "Thinking", "Thinking", "Answer: 42", "Answer: 42", "Answer: 42"}}
	d := newTestDriver(p)
	var deltas []string
	res, err := d.Chat(context.Background(), "q", ChatOptions{Timeout: 5 * time.Second,
		OnDelta: func(s string) { deltas = append(deltas, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "Answer: 42" {
		t.Fatalf("reply = %q", res.Reply)
	}
	for i := 1; i < len(deltas); i++ {
		if !strings.HasPrefix(deltas[i], deltas[i-1]) {
			t.Fatalf("streamed a rewrite: %v", deltas)
		}
	}
}

func TestChat_EmptyPromptRejected(t *testing.T) {
	d := newTestDriver(&fakePage{url: "https://muse.ai/"})
	if _, err := d.Chat(context.Background(), "   ", ChatOptions{}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestChat_ErrorNoticeSurfaces(t *testing.T) {
	p := &fakePage{url: "https://muse.ai/", loggedIn: true, errNotice: "Something went wrong"}
	d := newTestDriver(p)
	_, err := d.Chat(context.Background(), "q", ChatOptions{Timeout: 2 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "Something went wrong") {
		t.Fatalf("err = %v", err)
	}
}

func TestChat_NewThreadClicksSideChat(t *testing.T) {
	p := &fakePage{url: "https://muse.ai/", loggedIn: true, steps: []string{"ok"}}
	d := newTestDriver(p)
	res, err := d.Chat(context.Background(), "q", ChatOptions{NewThread: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.ThreadURL, "/thread/") {
		t.Fatalf("thread url = %q", res.ThreadURL)
	}
}

func TestChat_AttachesDataURLFile(t *testing.T) {
	p := &fakePage{url: "https://muse.ai/", loggedIn: true, steps: []string{"a cat"}}
	d := newTestDriver(p)
	_, err := d.Chat(context.Background(), "what is this?", ChatOptions{Timeout: 5 * time.Second,
		Files: []string{"data:image/png;base64,iVBORw0KGgo="}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 1 || !strings.HasSuffix(p.files[0], ".png") {
		t.Fatalf("files = %v", p.files)
	}
	if _, err := os.Stat(p.files[0]); !os.IsNotExist(err) {
		t.Fatalf("temp attachment not cleaned up: %v", err)
	}
}

func TestStatusAndLogin(t *testing.T) {
	p := &fakePage{url: "about:blank", loggedIn: true}
	d := newTestDriver(p)
	st := d.Status(context.Background())
	if !st.BrowserRunning || !st.LoggedIn || !st.ComposerReady || st.ViewerID != "viewer-1" {
		t.Fatalf("status = %+v", st)
	}
	if len(p.navigated) == 0 || p.navigated[0] != AppURL {
		t.Fatalf("expected navigation to app, got %v", p.navigated)
	}
	if a, err := d.Login(context.Background(), time.Second); err != nil || !a.OK {
		t.Fatalf("login = %+v %v", a, err)
	}
}

func TestThreadURL(t *testing.T) {
	cases := map[string]string{
		"https://muse.ai/thread/abc":           "https://muse.ai/thread/abc",
		"11111111-2222-3333-4444-555555555555": "https://muse.ai/thread/11111111-2222-3333-4444-555555555555",
		"My side chat":                         "",
		"3":                                    "",
	}
	for in, want := range cases {
		if got := ThreadURL(in); got != want {
			t.Errorf("ThreadURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaterializeFiles(t *testing.T) {
	local := t.TempDir() + "/a.txt"
	if err := os.WriteFile(local, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, cleanup, err := materializeFiles(context.Background(), []string{local, "data:text/plain,hello%20world"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(paths) != 2 || paths[0] != local {
		t.Fatalf("paths = %v", paths)
	}
	b, _ := os.ReadFile(paths[1])
	if string(b) != "hello world" {
		t.Fatalf("data url decoded to %q", b)
	}
	if _, _, err := materializeFiles(context.Background(), []string{"/definitely/missing.png"}); err == nil {
		t.Fatal("expected error for missing file")
	}
}
