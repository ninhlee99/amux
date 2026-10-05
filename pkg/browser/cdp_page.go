package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/types"

	"github.com/gorilla/websocket"
)

// Session is a long-lived Chromium instance driven over the DevTools Protocol.
//
// Unlike the one-shot cookie capture in cdp_login.go, a Session keeps the
// browser (and its logged-in profile) alive so a web app whose chat is not a
// plain HTTP API — e.g. Meta Muse's encrypted WebSocket — can be driven by
// typing into the real page. Authentication lives entirely in the dedicated
// profile under ~/.amux/browser-profiles/<name>: no OS keychain is read.
type Session struct {
	Profile string // profile directory name under browser-profiles/
	cmd     *exec.Cmd
	port    int
	owned   bool // true when this process launched the browser
}

// SessionOptions controls how a Session is obtained.
type SessionOptions struct {
	// Profile is the subdirectory of ~/.amux/browser-profiles holding the login.
	Profile string
	// StartURL is opened in the first window when the browser is launched.
	StartURL string
	// Headless launches with --headless=new (only works after a first headful login).
	Headless bool
	// Endpoint attaches to an already-running Chrome (http://127.0.0.1:9222)
	// instead of launching one. The browser is never killed on Close.
	Endpoint string
	// LaunchTimeout bounds how long to wait for DevTools to come up.
	LaunchTimeout time.Duration
}

// OpenSession attaches to a running browser for opts.Profile when one is
// already up (another amux process launched it — Chrome records its port in
// <profile>/DevToolsActivePort), otherwise launches a new one. Sharing one
// browser per profile is what lets the gateway daemon and `amux mcp` use the
// same login without fighting over Chrome's profile lock.
func OpenSession(opts SessionOptions) (*Session, error) {
	if opts.LaunchTimeout <= 0 {
		opts.LaunchTimeout = 30 * time.Second
	}
	if ep := strings.TrimSpace(opts.Endpoint); ep != "" {
		port, err := portFromEndpoint(ep)
		if err != nil {
			return nil, err
		}
		if _, err := debuggerWSURL(port); err != nil {
			return nil, fmt.Errorf("no DevTools endpoint at %s: %w", ep, err)
		}
		return &Session{Profile: opts.Profile, port: port}, nil
	}

	dir, err := profileDir(opts.Profile)
	if err != nil {
		return nil, err
	}
	if port, ok := activeDevToolsPort(dir); ok {
		return &Session{Profile: opts.Profile, port: port}, nil
	}

	bin, err := findChromiumBinary()
	if err != nil {
		return nil, err
	}
	clearChromiumSingletonLocks(dir)
	_ = os.Remove(filepath.Join(dir, "DevToolsActivePort"))

	args := []string{
		"--remote-debugging-port=0", // Chrome picks a port and writes DevToolsActivePort
		"--user-data-dir=" + dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-blink-features=AutomationControlled",
		"--disable-features=Translate,OptimizationGuideModelDownloading",
		// Background tabs must keep rendering: replies stream into the DOM.
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	if opts.StartURL != "" {
		args = append(args, opts.StartURL)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}

	deadline := time.Now().Add(opts.LaunchTimeout)
	for time.Now().Before(deadline) {
		if port, ok := activeDevToolsPort(dir); ok {
			return &Session{Profile: opts.Profile, cmd: cmd, port: port, owned: true}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	terminateBrowser(cmd)
	return nil, fmt.Errorf("browser DevTools did not come up within %s", opts.LaunchTimeout)
}

// Owned reports whether Close will terminate the browser process.
func (s *Session) Owned() bool { return s != nil && s.owned }

// Alive reports whether the DevTools endpoint still answers.
func (s *Session) Alive() bool {
	if s == nil {
		return false
	}
	_, err := debuggerWSURL(s.port)
	return err == nil
}

// Close terminates a browser this process launched; an attached browser is
// only disconnected from, never killed.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.owned {
		terminateBrowser(s.cmd)
	}
}

// pageTarget is one row of /json/list.
type pageTarget struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	WSURL string `json:"webSocketDebuggerUrl"`
}

func (s *Session) listTargets() ([]pageTarget, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", s.port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out []pageTarget
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Session) newTarget(startURL string) (*pageTarget, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", s.port, url.QueryEscape(startURL)), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var t pageTarget
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}
	return &t, nil
}

// OpenPage connects to a tab. When reuse is true an existing tab whose URL
// starts with urlPrefix is taken over; otherwise (or when none matches) a new
// tab is opened at startURL. Separate processes should pass reuse=false so
// each drives its own tab and they never type into the same composer.
func (s *Session) OpenPage(ctx context.Context, urlPrefix, startURL string, reuse bool) (*Page, error) {
	var target *pageTarget
	if reuse {
		if ts, err := s.listTargets(); err == nil {
			for i := range ts {
				if ts[i].Type == "page" && strings.HasPrefix(ts[i].URL, urlPrefix) && ts[i].WSURL != "" {
					target = &ts[i]
					break
				}
			}
		}
	}
	if target == nil {
		t, err := s.newTarget(startURL)
		if err != nil {
			return nil, err
		}
		target = t
	}
	p, err := DialPage(ctx, target.WSURL)
	if err != nil {
		return nil, err
	}
	p.targetID = target.ID
	p.closeTab = func() {
		client := &http.Client{Timeout: 3 * time.Second}
		if resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", s.port, target.ID)); err == nil {
			resp.Body.Close()
		}
	}
	// Keep the page "focused" for rendering even when the window is behind others.
	_ = p.Call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, nil)
	_ = p.Call(ctx, "Page.enable", nil, nil)
	return p, nil
}

// activeDevToolsPort reads <dir>/DevToolsActivePort and verifies it answers.
func activeDevToolsPort(dir string) (int, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
	if err != nil {
		return 0, false
	}
	first := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	port, err := strconv.Atoi(first)
	if err != nil || port <= 0 {
		return 0, false
	}
	if _, err := debuggerWSURL(port); err != nil {
		return 0, false
	}
	return port, true
}

func portFromEndpoint(ep string) (int, error) {
	if !strings.Contains(ep, "://") {
		ep = "http://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil {
		return 0, fmt.Errorf("bad CDP endpoint %q: %w", ep, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("bad CDP endpoint %q: missing port", ep)
	}
	return port, nil
}

// ---------------------------------------------------------------------------
// Page: one CDP connection to one tab.
// ---------------------------------------------------------------------------

// Page is a DevTools connection to a single tab. Calls are safe for
// concurrent use; responses are matched to requests by id.
type Page struct {
	conn     *websocket.Conn
	writeMu  sync.Mutex
	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan cdpReply
	done     chan struct{}
	err      error
	targetID string
	closeTab func()
}

type cdpReply struct {
	Result json.RawMessage
	Err    *cdpError
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp %d: %s", e.Code, e.Message) }

// ErrPageClosed is returned once the tab connection has gone away.
var ErrPageClosed = errors.New("browser tab connection closed")

// DialPage opens a CDP connection to a page target's websocket URL.
func DialPage(ctx context.Context, wsURL string) (*Page, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect tab: %w", err)
	}
	conn.SetReadLimit(64 << 20)
	p := &Page{conn: conn, pending: map[int64]chan cdpReply{}, done: make(chan struct{})}
	go p.readLoop()
	return p, nil
}

func (p *Page) readLoop() {
	defer close(p.done)
	for {
		var msg struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *cdpError       `json:"error"`
		}
		if err := p.conn.ReadJSON(&msg); err != nil {
			p.mu.Lock()
			p.err = err
			for id, ch := range p.pending {
				close(ch)
				delete(p.pending, id)
			}
			p.mu.Unlock()
			return
		}
		if msg.ID == 0 {
			continue // event; this driver polls instead of subscribing
		}
		p.mu.Lock()
		ch := p.pending[msg.ID]
		delete(p.pending, msg.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- cdpReply{Result: msg.Result, Err: msg.Error}
		}
	}
}

// Closed reports whether the connection has dropped.
func (p *Page) Closed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Call sends one CDP command and decodes its result into out (may be nil).
func (p *Page) Call(ctx context.Context, method string, params any, out any) error {
	if p.Closed() {
		return ErrPageClosed
	}
	ch := make(chan cdpReply, 1)
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.pending[id] = ch
	p.mu.Unlock()

	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	p.writeMu.Lock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := p.conn.WriteJSON(req)
	p.writeMu.Unlock()
	if err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return ctx.Err()
	case r, ok := <-ch:
		if !ok {
			return ErrPageClosed
		}
		if r.Err != nil {
			return fmt.Errorf("%s: %w", method, r.Err)
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	}
}

// Eval runs a JavaScript expression in the page (awaiting promises) and
// decodes its JSON-serialisable value into out (may be nil).
func (p *Page) Eval(ctx context.Context, expr string, out any) error {
	var res struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := p.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
		"userGesture":   true,
	}, &res)
	if err != nil {
		return err
	}
	if ex := res.ExceptionDetails; ex != nil {
		msg := ex.Text
		if ex.Exception != nil && ex.Exception.Description != "" {
			msg = ex.Exception.Description
		}
		return fmt.Errorf("page script: %s", msg)
	}
	if out != nil && len(res.Result.Value) > 0 {
		return json.Unmarshal(res.Result.Value, out)
	}
	return nil
}

// Navigate loads url in the tab (does not wait for the app to hydrate).
func (p *Page) Navigate(ctx context.Context, rawURL string) error {
	return p.Call(ctx, "Page.navigate", map[string]any{"url": rawURL}, nil)
}

// URL returns the tab's current location.
func (p *Page) URL(ctx context.Context) (string, error) {
	var u string
	err := p.Eval(ctx, "location.href", &u)
	return u, err
}

// InsertText types text at the focused element as one IME commit — O(1) for
// large prompts and understood by rich editors (Lexical, ProseMirror).
func (p *Page) InsertText(ctx context.Context, text string) error {
	return p.Call(ctx, "Input.insertText", map[string]any{"text": text}, nil)
}

// keyDefs maps the key names this package supports to DevTools key events.
var keyDefs = map[string]struct {
	code string
	vk   int
	text string
}{
	"Enter":     {"Enter", 13, "\r"},
	"Backspace": {"Backspace", 8, ""},
	"Delete":    {"Delete", 46, ""},
	"Escape":    {"Escape", 27, ""},
}

// PressKey dispatches keyDown/keyUp for a named key ("Enter", "Escape", ...).
// modifiers is the CDP bitmask (1=Alt, 2=Ctrl, 4=Meta, 8=Shift).
func (p *Page) PressKey(ctx context.Context, key string, modifiers int) error {
	d, ok := keyDefs[key]
	if !ok {
		return fmt.Errorf("unsupported key %q", key)
	}
	down := map[string]any{
		"type": "keyDown", "key": key, "code": d.code,
		"windowsVirtualKeyCode": d.vk, "nativeVirtualKeyCode": d.vk, "modifiers": modifiers,
	}
	if d.text != "" && modifiers == 0 {
		down["text"] = d.text
		down["unmodifiedText"] = d.text
	}
	if err := p.Call(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return p.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyUp", "key": key, "code": d.code,
		"windowsVirtualKeyCode": d.vk, "nativeVirtualKeyCode": d.vk, "modifiers": modifiers,
	}, nil)
}

// SetFileInputFiles attaches local files to the first <input type=file>
// matching selector, without opening an OS file dialog.
func (p *Page) SetFileInputFiles(ctx context.Context, selector string, paths []string) error {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := p.Call(ctx, "DOM.getDocument", map[string]any{"depth": 0}, &doc); err != nil {
		return err
	}
	var q struct {
		NodeID int `json:"nodeId"`
	}
	if err := p.Call(ctx, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": selector}, &q); err != nil {
		return err
	}
	if q.NodeID == 0 {
		return fmt.Errorf("no element matches %s", selector)
	}
	return p.Call(ctx, "DOM.setFileInputFiles", map[string]any{"nodeId": q.NodeID, "files": paths}, nil)
}

// Close drops the connection; when closeTab is true the tab is closed too.
func (p *Page) Close(closeTab bool) {
	if p == nil {
		return
	}
	_ = p.conn.Close()
	if closeTab && p.closeTab != nil {
		p.closeTab()
	}
}

// ProfileExists reports whether ~/.amux/browser-profiles/<name> has been
// created (i.e. a login window was opened for it at least once).
func ProfileExists(name string) bool {
	entries, err := os.ReadDir(filepath.Join(types.BaseDir(), "browser-profiles", name))
	return err == nil && len(entries) > 0
}
