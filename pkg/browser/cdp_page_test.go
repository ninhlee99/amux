package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeCDP answers a handful of DevTools methods and records what it saw.
type fakeCDP struct {
	mu      sync.Mutex
	methods []string
	params  []map[string]any
}

func (f *fakeCDP) handler(t *testing.T) http.HandlerFunc {
	up := websocket.Upgrader{}
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		for {
			var req struct {
				ID     int64          `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := c.ReadJSON(&req); err != nil {
				return
			}
			f.mu.Lock()
			f.methods = append(f.methods, req.Method)
			f.params = append(f.params, req.Params)
			f.mu.Unlock()
			// An unsolicited event first: the client must skip it.
			_ = c.WriteJSON(map[string]any{"method": "Page.frameNavigated", "params": map[string]any{}})
			resp := map[string]any{"id": req.ID}
			switch req.Method {
			case "Runtime.evaluate":
				expr, _ := req.Params["expression"].(string)
				switch expr {
				case "boom":
					resp["result"] = map[string]any{"result": map[string]any{"type": "object"},
						"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "Error: boom"}}}
				default:
					resp["result"] = map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"echo": expr, "n": 7}}}
				}
			case "DOM.getDocument":
				resp["result"] = map[string]any{"root": map[string]any{"nodeId": 1}}
			case "DOM.querySelector":
				if req.Params["selector"] == "#missing" {
					resp["result"] = map[string]any{"nodeId": 0}
				} else {
					resp["result"] = map[string]any{"nodeId": 42}
				}
			case "Bad.method":
				resp["error"] = map[string]any{"code": -32601, "message": "not found"}
			default:
				resp["result"] = map[string]any{}
			}
			_ = c.WriteJSON(resp)
		}
	}
}

func dialFake(t *testing.T) (*Page, *fakeCDP, func()) {
	t.Helper()
	f := &fakeCDP{}
	srv := httptest.NewServer(f.handler(t))
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	p, err := DialPage(context.Background(), ws)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	return p, f, func() { p.Close(false); srv.Close() }
}

func TestPage_EvalDecodesValue(t *testing.T) {
	p, _, done := dialFake(t)
	defer done()
	var out struct {
		Echo string `json:"echo"`
		N    int    `json:"n"`
	}
	if err := p.Eval(context.Background(), "1+1", &out); err != nil {
		t.Fatal(err)
	}
	if out.Echo != "1+1" || out.N != 7 {
		t.Fatalf("out = %+v", out)
	}
}

func TestPage_EvalException(t *testing.T) {
	p, _, done := dialFake(t)
	defer done()
	err := p.Eval(context.Background(), "boom", nil)
	if err == nil || !strings.Contains(err.Error(), "Error: boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestPage_CallError(t *testing.T) {
	p, _, done := dialFake(t)
	defer done()
	if err := p.Call(context.Background(), "Bad.method", nil, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestPage_PressKeyAndInsertText(t *testing.T) {
	p, f, done := dialFake(t)
	defer done()
	ctx := context.Background()
	if err := p.InsertText(ctx, "héllo"); err != nil {
		t.Fatal(err)
	}
	if err := p.PressKey(ctx, "Enter", 0); err != nil {
		t.Fatal(err)
	}
	if err := p.PressKey(ctx, "F13", 0); err == nil {
		t.Fatal("expected unsupported key error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := []string{"Input.insertText", "Input.dispatchKeyEvent", "Input.dispatchKeyEvent"}
	if strings.Join(f.methods, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v", f.methods)
	}
	if f.params[0]["text"] != "héllo" || f.params[1]["type"] != "keyDown" || f.params[1]["text"] != "\r" || f.params[2]["type"] != "keyUp" {
		b, _ := json.Marshal(f.params)
		t.Fatalf("params = %s", b)
	}
}

func TestPage_SetFileInputFiles(t *testing.T) {
	p, f, done := dialFake(t)
	defer done()
	ctx := context.Background()
	if err := p.SetFileInputFiles(ctx, "input[type=file]", []string{"/tmp/a.png"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetFileInputFiles(ctx, "#missing", []string{"/tmp/a.png"}); err == nil {
		t.Fatal("expected error for missing element")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.methods[2] != "DOM.setFileInputFiles" || f.params[2]["nodeId"].(float64) != 42 {
		t.Fatalf("methods = %v params = %v", f.methods, f.params)
	}
}

func TestPage_ClosedConnectionFailsFast(t *testing.T) {
	p, _, done := dialFake(t)
	done()
	deadline := time.Now().Add(2 * time.Second)
	for !p.Closed() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Call(ctx, "Runtime.evaluate", nil, nil); err == nil {
		t.Fatal("expected error on closed page")
	}
}

func TestPortFromEndpoint(t *testing.T) {
	for in, want := range map[string]int{"http://127.0.0.1:9222": 9222, "127.0.0.1:9333": 9333, "ws://localhost:9229/devtools": 9229} {
		got, err := portFromEndpoint(in)
		if err != nil || got != want {
			t.Errorf("portFromEndpoint(%q) = %d, %v", in, got, err)
		}
	}
	if _, err := portFromEndpoint("http://127.0.0.1"); err == nil {
		t.Error("expected error without port")
	}
}
