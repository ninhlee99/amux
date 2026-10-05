// Package muse drives Meta Muse (https://muse.ai, the "Hatch" agent) through
// a real, logged-in Chromium tab over the DevTools Protocol.
//
// Muse has no public API: its chat runs over an end-to-end encrypted Noise
// WebSocket whose keys live inside the web app. Rather than re-implementing
// that transport, the driver lets the page do the crypto and talks to it the
// way a person does — type into the composer, press Enter, read the rendered
// reply. Login lives only in the dedicated browser profile
// (~/.amux/browser-profiles/muse); nothing is read from the OS keychain.
//
// Invariants:
//   - Every browser action is serialised through Driver.mu (one composer).
//   - Prompts are sent verbatim — no "### USER"/"Assistant:" role-play
//     framing, which Muse treats as prompt injection.
//   - Streaming only ever reports stable, monotonic text.
//   - A browser this process merely attached to is never killed.
package muse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/browser"
)

// AppURL is the Muse web app origin.
const AppURL = "https://muse.ai/"

// DefaultModel is the model id amux reports for Muse.
const DefaultModel = "muse-spark"

// Selectors for the Muse web app (data-* attributes, stable across builds).
var Selectors = struct {
	ComposerRoot, Editor, StopButton, ActionSlot, FileInput,
	Message, ThreadRow, Assistant, User, ErrorNotice, ApprovalStack string
}{
	ComposerRoot:  `[data-hatch-composer-root]`,
	Editor:        `[data-lexical-editor="true"], [data-hatch-composer-root] textarea, [data-hatch-composer-prehydration-input], [data-hatch-composer-root] [contenteditable="true"]`,
	StopButton:    `[data-testid="hatch-composer-stop-button"]`,
	ActionSlot:    `[data-hatch-composer-action-slot]`,
	FileInput:     `[data-hatch-composer-root] input[type="file"]`,
	Message:       `[data-message-item]`,
	ThreadRow:     `[data-testid="hatch-thread-row"]`,
	Assistant:     `[data-message-item][data-message-role="assistant"]`,
	User:          `[data-message-item][data-message-role="user"]`,
	ErrorNotice:   `[data-testid="assistant-response-error-notice"]`,
	ApprovalStack: `[data-hatch-composer-approval-stack]`,
}

// Page is the subset of a DevTools tab the driver needs (browser.Page in
// production, a fake in tests).
type Page interface {
	Eval(ctx context.Context, expr string, out any) error
	Navigate(ctx context.Context, url string) error
	InsertText(ctx context.Context, text string) error
	PressKey(ctx context.Context, key string, modifiers int) error
	SetFileInputFiles(ctx context.Context, selector string, paths []string) error
	Closed() bool
	Close(closeTab bool)
}

// Config controls how the driver reaches the browser.
type Config struct {
	// AppURL is the web app root (default https://muse.ai/; AMUX_MUSE_URL).
	AppURL   string
	Profile  string // browser profile name (default "muse")
	Endpoint string // attach to this DevTools endpoint instead of launching
	Headless bool
	// QuietPeriod: text must be unchanged this long before it is streamed.
	QuietPeriod time.Duration
	// DoneIdle / NoStopIdle: idle time after which a reply counts as finished
	// once the stop button is gone (seen / never seen while generating).
	DoneIdle   time.Duration
	NoStopIdle time.Duration
	Poll       time.Duration
}

// ConfigFromEnv applies AMUX_MUSE_* overrides on top of defaults.
func ConfigFromEnv() Config {
	c := Config{
		AppURL:   os.Getenv("AMUX_MUSE_URL"),
		Profile:  envOr("AMUX_MUSE_PROFILE", "muse"),
		Endpoint: os.Getenv("AMUX_MUSE_CDP"),
		Headless: os.Getenv("AMUX_MUSE_HEADLESS") == "1",
	}
	if ms, err := strconv.Atoi(os.Getenv("AMUX_MUSE_STREAM_QUIET_MS")); err == nil && ms > 0 {
		c.QuietPeriod = time.Duration(ms) * time.Millisecond
	}
	return c
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func (c *Config) defaults() {
	if c.AppURL == "" {
		c.AppURL = AppURL
	}
	if !strings.HasSuffix(c.AppURL, "/") {
		c.AppURL += "/"
	}
	if c.Profile == "" {
		c.Profile = "muse"
	}
	if c.QuietPeriod <= 0 {
		c.QuietPeriod = 600 * time.Millisecond
	}
	if c.DoneIdle <= 0 {
		c.DoneIdle = 1200 * time.Millisecond
	}
	if c.NoStopIdle <= 0 {
		c.NoStopIdle = 2500 * time.Millisecond
	}
	if c.Poll <= 0 {
		c.Poll = 300 * time.Millisecond
	}
}

// Driver owns one Muse tab. Use Shared to get the process-wide instance.
type Driver struct {
	cfg  Config
	mu   sync.Mutex
	sess *browser.Session
	page Page
	// open returns a fresh tab; replaced in tests.
	open func(ctx context.Context) (Page, error)
	// sleep is replaced in tests.
	sleep func(time.Duration)
}

// New returns a driver that launches/attaches Chromium on first use.
func New(cfg Config) *Driver {
	cfg.defaults()
	d := &Driver{cfg: cfg, sleep: time.Sleep}
	d.open = d.openBrowserPage
	return d
}

// NewWithPage returns a driver bound to an existing page (tests, embedding).
func NewWithPage(cfg Config, p Page) *Driver {
	cfg.defaults()
	d := &Driver{cfg: cfg, page: p, sleep: time.Sleep}
	d.open = func(context.Context) (Page, error) {
		if d.page != nil && !d.page.Closed() {
			return d.page, nil
		}
		return nil, errors.New("muse: page closed")
	}
	return d
}

var (
	sharedMu sync.Mutex
	shared   = map[string]*Driver{}
)

// Shared returns the process-wide driver for cfg.Profile, so the gateway's
// adapters and MCP tools never fight over one composer.
func Shared(cfg Config) *Driver {
	cfg.defaults()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	key := cfg.Profile + "|" + cfg.Endpoint
	if d, ok := shared[key]; ok {
		return d
	}
	d := New(cfg)
	shared[key] = d
	return d
}

func (d *Driver) openBrowserPage(ctx context.Context) (Page, error) {
	if d.sess == nil || !d.sess.Alive() {
		s, err := browser.OpenSession(browser.SessionOptions{
			Profile:  d.cfg.Profile,
			StartURL: d.cfg.AppURL,
			Headless: d.cfg.Headless,
			Endpoint: d.cfg.Endpoint,
		})
		if err != nil {
			return nil, fmt.Errorf("muse: open browser: %w", err)
		}
		d.sess = s
	}
	// The browser we launched opened a Muse tab: take it over. When attached
	// to someone else's browser, open our own tab so we never type into theirs.
	return d.sess.OpenPage(ctx, d.origin(), d.cfg.AppURL, d.sess.Owned())
}

// requirePage returns a live tab, (re)opening one if needed. Caller holds mu.
func (d *Driver) requirePage(ctx context.Context) (Page, error) {
	if d.page != nil && !d.page.Closed() {
		return d.page, nil
	}
	p, err := d.open(ctx)
	if err != nil {
		return nil, err
	}
	d.page = p
	return p, nil
}

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (d *Driver) url(ctx context.Context, p Page) string {
	var u string
	_ = p.Eval(ctx, "location.href", &u)
	return u
}

// gotoApp makes sure the tab is somewhere on muse.ai.
func (d *Driver) gotoApp(ctx context.Context, p Page) error {
	if strings.HasPrefix(d.url(ctx, p), d.origin()) {
		return nil
	}
	if err := p.Navigate(ctx, d.cfg.AppURL); err != nil {
		return err
	}
	return d.waitLoaded(ctx, p, 30*time.Second)
}

func (d *Driver) waitLoaded(ctx context.Context, p Page, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var state string
		if err := p.Eval(ctx, "document.readyState", &state); err == nil && state != "loading" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		d.sleep(d.cfg.Poll)
	}
	return errors.New("muse: page did not finish loading")
}

// waitComposer waits for the editor to be visible and not aria-busy.
func (d *Driver) waitComposer(ctx context.Context, p Page, timeout time.Duration) error {
	expr := fmt.Sprintf(`(() => {
  const ed = document.querySelector(%s);
  if (!ed) return 'missing';
  const r = ed.getBoundingClientRect();
  if (r.width === 0 && r.height === 0) return 'hidden';
  const root = document.querySelector(%s);
  if (root && root.getAttribute('aria-busy') === 'true') return 'busy';
  return 'ready';
})()`, js(Selectors.Editor), js(Selectors.ComposerRoot))
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		if err := p.Eval(ctx, expr, &last); err == nil && last == "ready" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		d.sleep(d.cfg.Poll)
	}
	return fmt.Errorf("muse: composer not ready (%s) — run `amux login muse` if signed out", last)
}

// AuthState is the result of the in-page cookie auth probe.
type AuthState struct {
	OK       bool   `json:"ok"`
	Status   int    `json:"status"`
	ViewerID string `json:"viewerId,omitempty"`
	Error    string `json:"error,omitempty"`
}

const authCheckJS = `(async () => {
  try {
    const res = await fetch('/api/auth/check', {method:'POST', headers:{'content-type':'application/json'}, body:'{}', credentials:'same-origin'});
    const j = await res.json().catch(() => null);
    return {status: res.status, ok: !!(j && j.ok), viewerId: (j && j.viewer_id) ? String(j.viewer_id) : ''};
  } catch (e) { return {status: 0, ok: false, error: String(e)}; }
})()`

func (d *Driver) checkAuth(ctx context.Context, p Page) AuthState {
	var a AuthState
	if err := p.Eval(ctx, authCheckJS, &a); err != nil {
		return AuthState{Error: err.Error()}
	}
	return a
}

// Status describes browser, login and composer state.
type Status struct {
	BrowserRunning bool   `json:"browserRunning"`
	URL            string `json:"url,omitempty"`
	LoggedIn       bool   `json:"loggedIn"`
	ViewerID       string `json:"viewerId,omitempty"`
	ComposerReady  bool   `json:"composerReady"`
	Profile        string `json:"profile"`
	Headless       bool   `json:"headless"`
	Attached       bool   `json:"attached"`
	Error          string `json:"error,omitempty"`
}

// Status opens the browser if needed and reports login/composer state.
func (d *Driver) Status(ctx context.Context) Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := Status{Profile: d.cfg.Profile, Headless: d.cfg.Headless, Attached: d.cfg.Endpoint != ""}
	p, err := d.requirePage(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.BrowserRunning = true
	if err := d.gotoApp(ctx, p); err != nil {
		st.Error = err.Error()
	}
	st.URL = d.url(ctx, p)
	a := d.checkAuth(ctx, p)
	st.LoggedIn, st.ViewerID = a.OK, a.ViewerID
	st.ComposerReady = d.waitComposer(ctx, p, 8*time.Second) == nil
	return st
}

// Login waits (up to timeout) for the user to finish signing in to Meta in
// the visible browser window. Returns the viewer id on success.
func (d *Driver) Login(ctx context.Context, timeout time.Duration) (AuthState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	p, err := d.requirePage(ctx)
	if err != nil {
		return AuthState{}, err
	}
	if err := d.gotoApp(ctx, p); err != nil {
		return AuthState{}, err
	}
	deadline := time.Now().Add(timeout)
	for {
		a := d.checkAuth(ctx, p)
		if a.OK {
			return a, nil
		}
		if time.Now().After(deadline) {
			return a, errors.New("muse: not signed in — finish the Meta login in the opened browser window, then retry")
		}
		if err := ctx.Err(); err != nil {
			return a, err
		}
		d.sleep(2 * time.Second)
	}
}

// NewChat starts a fresh side chat (falls back to the home composer).
func (d *Driver) NewChat(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.requirePage(ctx)
	if err != nil {
		return "", err
	}
	if err := d.newChat(ctx, p); err != nil {
		return "", err
	}
	return d.url(ctx, p), nil
}

func (d *Driver) newChat(ctx context.Context, p Page) error {
	if err := d.gotoApp(ctx, p); err != nil {
		return err
	}
	var clicked bool
	_ = p.Eval(ctx, `(() => {
  const b = [...document.querySelectorAll('button,[role="button"]')].find(el => /new side chat/i.test((el.getAttribute('aria-label')||'') + ' ' + (el.innerText||'')));
  if (!b) return false; b.click(); return true;
})()`, &clicked)
	if clicked {
		d.sleep(1200 * time.Millisecond)
	} else {
		if err := p.Navigate(ctx, d.cfg.AppURL); err != nil {
			return err
		}
		if err := d.waitLoaded(ctx, p, 30*time.Second); err != nil {
			return err
		}
	}
	return d.waitComposer(ctx, p, 90*time.Second)
}

// origin is the app's scheme://host (no trailing slash).
func (d *Driver) origin() string {
	return strings.TrimSuffix(d.cfg.AppURL, "/")
}

var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ThreadURL turns a target (URL, thread id) into a navigable muse.ai URL, or
// "" when the target must be resolved against the sidebar (title or index).
func ThreadURL(target string) string {
	return threadURL(AppURL, target)
}

func threadURL(appURL, target string) string {
	s := strings.TrimSpace(target)
	origin := strings.TrimSuffix(appURL, "/")
	switch {
	case strings.HasPrefix(s, origin):
		return s
	case uuidRe.MatchString(s):
		return origin + "/thread/" + s
	}
	return ""
}

// OpenChat opens a chat by sidebar index, title substring, thread URL or id.
// An empty target opens the home/main chat.
func (d *Driver) OpenChat(ctx context.Context, target string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.requirePage(ctx)
	if err != nil {
		return "", err
	}
	if err := d.openChat(ctx, p, target); err != nil {
		return "", err
	}
	return d.url(ctx, p), nil
}

func (d *Driver) openChat(ctx context.Context, p Page, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		if err := d.gotoApp(ctx, p); err != nil {
			return err
		}
		return d.waitComposer(ctx, p, 90*time.Second)
	}
	if u := threadURL(d.cfg.AppURL, target); u != "" {
		if d.url(ctx, p) != u {
			if err := p.Navigate(ctx, u); err != nil {
				return err
			}
			if err := d.waitLoaded(ctx, p, 30*time.Second); err != nil {
				return err
			}
			d.sleep(1200 * time.Millisecond)
		}
		return d.waitComposer(ctx, p, 90*time.Second)
	}
	if err := d.gotoApp(ctx, p); err != nil {
		return err
	}
	idx := -1
	if n, err := strconv.Atoi(target); err == nil {
		idx = n
	}
	var res string
	expr := fmt.Sprintf(`(() => {
  const rows = [...document.querySelectorAll(%s)];
  const idx = %d, q = %s.toLowerCase();
  const row = idx >= 0 ? rows[idx] : rows.find(r => (r.innerText||'').toLowerCase().includes(q));
  if (!row) return idx >= 0 ? 'index out of range' : 'no chat matching';
  (row.querySelector('a,button') || row).click();
  return 'ok';
})()`, js(Selectors.ThreadRow), idx, js(target))
	if err := p.Eval(ctx, expr, &res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("muse: %s %q", res, target)
	}
	d.sleep(1200 * time.Millisecond)
	return d.waitComposer(ctx, p, 90*time.Second)
}

// ChatOptions controls one Chat call.
type ChatOptions struct {
	Timeout   time.Duration
	NewThread bool
	Chat      string   // target chat (see OpenChat); ignored with NewThread
	Files     []string // local paths, file:// / http(s):// / data: URLs
	// OnDelta receives the cumulative stable reply text as it grows.
	OnDelta func(full string)
}

// ChatResult is the outcome of one Chat call.
type ChatResult struct {
	Reply         string   `json:"reply"`
	Messages      []string `json:"messages"`
	ThreadURL     string   `json:"threadUrl"`
	ElapsedMs     int64    `json:"elapsedMs"`
	Error         string   `json:"error,omitempty"`
	TimedOut      bool     `json:"timedOut,omitempty"`
	NeedsApproval bool     `json:"needsApproval,omitempty"`
}

type snapshot struct {
	N        int    `json:"n"`
	Last     string `json:"last"`
	Stop     bool   `json:"stop"`
	Err      string `json:"err"`
	Approval bool   `json:"approval"`
	Composer string `json:"composer"`
}

var snapshotJS = fmt.Sprintf(`(() => {
  const a = document.querySelectorAll(%s);
  const ed = document.querySelector(%s);
  const composer = ed ? ((ed.tagName === 'TEXTAREA' || ed.tagName === 'INPUT') ? ed.value : ed.innerText) : '';
  const err = document.querySelector(%s);
  return {n: a.length, last: a.length ? (a[a.length-1].innerText || '') : '', stop: !!document.querySelector(%s),
          err: err ? (err.innerText || 'assistant error') : '', approval: !!document.querySelector(%s), composer: composer || ''};
})()`, js(Selectors.Assistant), js(Selectors.Editor), js(Selectors.ErrorNotice), js(Selectors.StopButton), js(Selectors.ApprovalStack))

func (d *Driver) snap(ctx context.Context, p Page) (snapshot, error) {
	var s snapshot
	err := p.Eval(ctx, snapshotJS, &s)
	return s, err
}

// focusAndClearJS focuses the composer and selects+deletes any draft.
var focusAndClearJS = fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) return false;
  el.focus();
  if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') { el.select(); }
  else { const r = document.createRange(); r.selectNodeContents(el); const s = getSelection(); s.removeAllRanges(); s.addRange(r); }
  document.execCommand('delete');
  return true;
})()`, js(Selectors.Editor))

// fallbackInsertJS is used when Input.insertText was ignored by the editor.
func fallbackInsertJS(text string) string {
	return fmt.Sprintf(`(() => {
  const el = document.querySelector(%s); if (!el) return false; el.focus();
  if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
    const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set;
    set.call(el, %s); el.dispatchEvent(new Event('input', {bubbles: true}));
  } else { document.execCommand('insertText', false, %s); }
  return true;
})()`, js(Selectors.Editor), js(text), js(text))
}

var clickSendJS = fmt.Sprintf(`(() => {
  const b = [...document.querySelectorAll(%s + ' button')].pop();
  if (!b) return false; b.click(); return true;
})()`, js(Selectors.ActionSlot))

// Chat sends prompt (verbatim) and waits for the assistant reply to finish.
func (d *Driver) Chat(ctx context.Context, prompt string, o ChatOptions) (*ChatResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(prompt) == "" && len(o.Files) == 0 {
		return nil, errors.New("muse: prompt is empty")
	}
	if o.Timeout <= 0 {
		o.Timeout = 4 * time.Minute
	}
	p, err := d.requirePage(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case o.NewThread:
		err = d.newChat(ctx, p)
	case strings.TrimSpace(o.Chat) != "":
		err = d.openChat(ctx, p, o.Chat)
	default:
		if err = d.gotoApp(ctx, p); err == nil {
			err = d.waitComposer(ctx, p, 90*time.Second)
		}
	}
	if err != nil {
		return nil, err
	}

	if len(o.Files) > 0 {
		paths, cleanup, err := materializeFiles(ctx, o.Files)
		defer cleanup()
		if err != nil {
			return nil, err
		}
		if err := p.SetFileInputFiles(ctx, Selectors.FileInput, paths); err != nil {
			return nil, fmt.Errorf("muse: attach files: %w", err)
		}
		d.sleep(800 * time.Millisecond) // let the app ingest + preview
	}

	before, err := d.snap(ctx, p)
	if err != nil {
		return nil, err
	}

	var focused bool
	if err := p.Eval(ctx, focusAndClearJS, &focused); err != nil || !focused {
		return nil, errors.New("muse: composer not found")
	}
	if prompt != "" {
		_ = p.InsertText(ctx, prompt)
		d.sleep(120 * time.Millisecond)
		if s, _ := d.snap(ctx, p); strings.TrimSpace(s.Composer) == "" {
			_ = p.Eval(ctx, fallbackInsertJS(prompt), nil)
		}
	}
	d.sleep(150 * time.Millisecond)

	t0 := time.Now()
	if err := p.PressKey(ctx, "Enter", 0); err != nil {
		return nil, err
	}
	sent := false
	for i := 0; i < 10; i++ {
		d.sleep(d.cfg.Poll)
		s, _ := d.snap(ctx, p)
		if strings.TrimSpace(s.Composer) == "" || s.Stop || s.N > before.N {
			sent = true
			break
		}
	}
	if !sent {
		_ = p.Eval(ctx, clickSendJS, nil)
	}

	res := &ChatResult{}
	deadline := t0.Add(o.Timeout)
	started := false
	lastText, committed := "", ""
	stableSince := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			res.TimedOut = true
			break
		}
		s, err := d.snap(ctx, p)
		if err != nil {
			if p.Closed() {
				return nil, browser.ErrPageClosed
			}
			d.sleep(d.cfg.Poll)
			continue
		}
		if s.Stop {
			started = true
		}
		if s.Err != "" {
			res.Error = strings.TrimSpace(s.Err)
		}
		if s.N > before.N {
			if s.Last != lastText {
				lastText = s.Last
				stableSince = time.Now()
			}
			idle := time.Since(stableSince)
			// Commit only stable, monotonic growth — never a rewritten draft.
			if o.OnDelta != nil && lastText != committed && strings.HasPrefix(lastText, committed) && idle >= d.cfg.QuietPeriod {
				committed = lastText
				o.OnDelta(committed)
			}
			if !s.Stop && ((started && idle > d.cfg.DoneIdle) || (!started && idle > d.cfg.NoStopIdle)) {
				break
			}
		} else if res.Error != "" && !s.Stop && time.Since(t0) > d.cfg.NoStopIdle {
			break // errored before any reply rendered
		}
		d.sleep(d.cfg.Poll)
	}

	var items []string
	_ = p.Eval(ctx, fmt.Sprintf(`[...document.querySelectorAll(%s)].slice(%d).map(e => (e.innerText||'').trim()).filter(Boolean)`,
		js(Selectors.Assistant), before.N), &items)
	reply := strings.TrimSpace(lastText)
	if len(items) > 0 {
		reply = items[len(items)-1]
	}
	if o.OnDelta != nil && reply != committed && strings.HasPrefix(reply, committed) {
		o.OnDelta(reply)
	}
	var approval bool
	_ = p.Eval(ctx, fmt.Sprintf(`!!document.querySelector(%s)`, js(Selectors.ApprovalStack)), &approval)

	res.Reply = reply
	res.Messages = items
	res.ThreadURL = d.url(ctx, p)
	res.ElapsedMs = time.Since(t0).Milliseconds()
	res.NeedsApproval = approval
	if res.Reply == "" && res.Error != "" {
		return res, fmt.Errorf("muse: %s", res.Error)
	}
	return res, nil
}

// Message is one rendered chat message.
type Message struct {
	Role  string   `json:"role"`
	Text  string   `json:"text"`
	Media []string `json:"media,omitempty"`
}

var readMessagesJS = fmt.Sprintf(`[...document.querySelectorAll(%s)].map(el => {
  const text = (el.innerText || '').replace(/^(You:|User message:|Assistant message:)\s*/i, '').trim();
  const media = [...new Set([...el.querySelectorAll('a[href], img[src], video[src], source[src]')]
    .map(n => n.href || n.currentSrc || n.src || n.getAttribute('src')).filter(u => u && /^(https?:|blob:)/i.test(u)))];
  return {role: el.getAttribute('data-message-role') || '', text, media};
}).filter(m => m.text || m.media.length)`, js(Selectors.Message))

// ReadChat returns up to max messages of a chat (current chat when target is "").
func (d *Driver) ReadChat(ctx context.Context, target string, max int) ([]Message, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.requirePage(ctx)
	if err != nil {
		return nil, "", err
	}
	if target != "" {
		if err := d.openChat(ctx, p, target); err != nil {
			return nil, "", err
		}
	} else if err := d.gotoApp(ctx, p); err != nil {
		return nil, "", err
	}
	var msgs []Message
	if err := p.Eval(ctx, readMessagesJS, &msgs); err != nil {
		return nil, "", err
	}
	if max > 0 && len(msgs) > max {
		msgs = msgs[len(msgs)-max:]
	}
	return msgs, d.url(ctx, p), nil
}

// ChatInfo is one sidebar row.
type ChatInfo struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// ListChats lists the sidebar chats (Main chat, Channels, Side chats).
func (d *Driver) ListChats(ctx context.Context, query string) ([]ChatInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.requirePage(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.gotoApp(ctx, p); err != nil {
		return nil, err
	}
	d.sleep(400 * time.Millisecond)
	var chats []ChatInfo
	err = p.Eval(ctx, fmt.Sprintf(`[...document.querySelectorAll(%s)].map((el, index) => ({
  index,
  title: (el.innerText || '').replace(/\s+/g, ' ').replace(/\s*More thread actions\s*$/i, '').trim(),
  active: el.getAttribute('aria-current') === 'page' || el.getAttribute('aria-selected') === 'true',
})).filter(c => c.title)`, js(Selectors.ThreadRow)), &chats)
	if err != nil {
		return nil, err
	}
	if q := strings.ToLower(strings.TrimSpace(query)); q != "" {
		out := chats[:0]
		for _, c := range chats {
			if strings.Contains(strings.ToLower(c.Title), q) {
				out = append(out, c)
			}
		}
		chats = out
	}
	return chats, nil
}

// DOMDump is a selector-debugging snapshot.
type DOMDump struct {
	URL    string         `json:"url"`
	Title  string         `json:"title"`
	Counts map[string]int `json:"counts"`
	HTML   string         `json:"html"`
}

// DumpDOM returns element counts and transcript HTML (for fixing selectors).
func (d *Driver) DumpDOM(ctx context.Context, maxChars int) (*DOMDump, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if maxChars <= 0 {
		maxChars = 20000
	}
	p, err := d.requirePage(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.gotoApp(ctx, p); err != nil {
		return nil, err
	}
	var out DOMDump
	err = p.Eval(ctx, fmt.Sprintf(`(() => {
  const q = s => document.querySelectorAll(s).length;
  const log = document.querySelector('[role="log"]'), first = document.querySelector(%s);
  const c = log || (first && first.parentElement) || document.body;
  return {url: location.href, title: document.title,
    counts: {messages: q(%s), assistant: q(%s), user: q(%s), editor: q(%s), threads: q(%s)},
    html: c.outerHTML.slice(0, %d)};
})()`, js(Selectors.Message), js(Selectors.Message), js(Selectors.Assistant), js(Selectors.User),
		js(Selectors.Editor), js(Selectors.ThreadRow), maxChars), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Close drops the tab connection and, when this process launched the browser,
// shuts it down. An attached browser is only disconnected from.
func (d *Driver) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.page != nil {
		d.page.Close(false)
		d.page = nil
	}
	if d.sess != nil {
		d.sess.Close()
		d.sess = nil
	}
}
