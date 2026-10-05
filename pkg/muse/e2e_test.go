//go:build e2e

// End-to-end check of the real CDP stack (browser launch, DevToolsActivePort
// attach, tab control, typing, Enter, file input, streaming detection, media
// download) against a local page that mimics Muse's DOM contract.
//
//	go test -tags e2e ./pkg/muse -run E2E -v
package muse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeMuseHTML = `<!doctype html><html><head><title>Fake Muse</title></head><body>
<nav>
  <div data-testid="hatch-thread-row" aria-current="page"><a href="#">Main chat</a></div>
  <div data-testid="hatch-thread-row"><a href="/thread/22222222-3333-4444-5555-666666666666">Design notes</a></div>
  <button aria-label="New side chat" onclick="location.href='/thread/11111111-2222-3333-4444-555555555555'">New side chat</button>
</nav>
<main role="log" id="log"></main>
<div data-hatch-composer-root>
  <input type="file" multiple id="file">
  <div data-lexical-editor="true" contenteditable="true" id="ed" style="min-height:20px;border:1px solid #ccc"></div>
  <div data-hatch-composer-action-slot><button id="send">Send</button></div>
</div>
<script>
const log = document.getElementById('log'), ed = document.getElementById('ed'), file = document.getElementById('file');
function msg(role, text) {
  const el = document.createElement('div');
  el.setAttribute('data-message-item', ''); el.setAttribute('data-message-role', role);
  el.innerText = text; log.appendChild(el); return el;
}
function send() {
  const text = ed.innerText.trim();
  if (!text && !file.files.length) return;
  const names = [...file.files].map(f => f.name + ':' + f.size).join(',');
  ed.innerText = ''; msg('user', text);
  const stop = document.createElement('button');
  stop.setAttribute('data-testid', 'hatch-composer-stop-button'); document.body.appendChild(stop);
  const a = msg('assistant', 'Thinking');
  const full = 'Echo: ' + text + (names ? ' [files ' + names + ']' : '');
  let i = 0;
  setTimeout(() => {
    a.innerText = '';
    const t = setInterval(() => {
      i = Math.min(full.length, i + 4); a.innerText = full.slice(0, i);
      if (i >= full.length) {
        clearInterval(t);
        if (text.includes('image')) { const l = document.createElement('a'); l.href = '/files/cat.png'; l.textContent = 'cat.png'; a.appendChild(l); }
        stop.remove();
      }
    }, 80);
  }, 400);
}
ed.addEventListener('keydown', e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } });
document.getElementById('send').onclick = send;
</script></body></html>`

func fakeMuseServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"viewer_id":"viewer-e2e"}`)
	})
	mux.HandleFunc("/files/cat.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fakeMuseHTML)
	})
	return httptest.NewServer(mux)
}

func TestE2E_RealChromeAgainstFakeMuse(t *testing.T) {
	t.Setenv("AMUX_HOME", t.TempDir())
	srv := fakeMuseServer()
	defer srv.Close()

	d := New(Config{AppURL: srv.URL, Profile: "e2e", Headless: true})
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	st := d.Status(ctx)
	if !st.BrowserRunning || !st.LoggedIn || !st.ComposerReady || st.ViewerID != "viewer-e2e" {
		t.Fatalf("status = %+v", st)
	}

	var deltas []string
	res, err := d.Chat(ctx, "hello from amux", ChatOptions{Timeout: time.Minute, OnDelta: func(s string) { deltas = append(deltas, s) }})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Reply != "Echo: hello from amux" {
		t.Fatalf("reply = %q", res.Reply)
	}
	for _, dl := range deltas {
		if strings.Contains(dl, "Thinking") {
			t.Fatalf("streamed the placeholder draft: %v", deltas)
		}
	}
	for i := 1; i < len(deltas); i++ {
		if !strings.HasPrefix(deltas[i], deltas[i-1]) {
			t.Fatalf("non-monotonic stream: %v", deltas)
		}
	}

	att := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(att, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = d.Chat(ctx, "describe this image", ChatOptions{Timeout: time.Minute, Files: []string{att, "data:text/plain,abc"}})
	if err != nil {
		t.Fatalf("chat with files: %v", err)
	}
	if !strings.Contains(res.Reply, "note.txt:5") || !strings.Contains(res.Reply, "attachment.txt:3") {
		t.Fatalf("attachments not delivered: %q", res.Reply)
	}

	chats, err := d.ListChats(ctx, "design")
	if err != nil || len(chats) != 1 || chats[0].Title != "Design notes" {
		t.Fatalf("chats = %+v, %v", chats, err)
	}

	media, err := d.Media(ctx, "", true, t.TempDir())
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	if len(media.Saved) != 1 || media.Saved[0].Error != "" || media.Saved[0].Bytes == 0 {
		t.Fatalf("media = %+v", media)
	}

	url, err := d.NewChat(ctx)
	if err != nil || !strings.Contains(url, "/thread/11111111") {
		t.Fatalf("new chat = %q, %v", url, err)
	}

	// A second driver (as `amux mcp` would be) attaches to the same browser.
	d2 := New(Config{AppURL: srv.URL, Profile: "e2e", Headless: true})
	if st2 := d2.Status(ctx); !st2.BrowserRunning || !st2.LoggedIn {
		t.Fatalf("second driver status = %+v", st2)
	}
	if d2.sess.Owned() {
		t.Fatal("second process must attach, not launch")
	}
	d2.Close()
	if !d.sess.Alive() {
		t.Fatal("closing an attached driver must not kill the browser")
	}
}
