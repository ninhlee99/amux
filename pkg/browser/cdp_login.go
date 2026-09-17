package browser

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"amux-accounts/pkg/types"

	"github.com/gorilla/websocket"
)

// WebLoginTarget describes a site to open and which cookie to wait for.
type WebLoginTarget struct {
	Name       string // chatgpt | claude
	StartURL   string
	CookieHost string // substring match on cookie domain
	CookieName string
	Profile    string // subdir under ~/.am/browser-profiles
}

var (
	ChatGPTWebLogin = WebLoginTarget{
		Name:       "chatgpt",
		StartURL:   "https://chatgpt.com/",
		CookieHost: "chatgpt.com",
		CookieName: "__Secure-next-auth.session-token",
		Profile:    "chatgpt",
	}
	ClaudeWebLogin = WebLoginTarget{
		Name:       "claude",
		StartURL:   "https://claude.ai/login",
		CookieHost: "claude.ai",
		CookieName: "sessionKey",
		Profile:    "claude",
	}
	GeminiWebLogin = WebLoginTarget{
		Name:       "gemini",
		StartURL:   "https://gemini.google.com/app",
		CookieHost: "google.com",
		CookieName: "__Secure-1PSID",
		Profile:    "gemini",
	}
)

// CapturedWebAuth is the session cookie plus a full Cookie header for the
// login host (Cloudflare / ancillary cookies included).
type CapturedWebAuth struct {
	SessionValue string
	CookieHeader string
}

// CaptureWebAuthViaBrowser opens a dedicated Chromium window (own profile under
// ~/.am/browser-profiles — NOT the system Chrome profile, NOT Keychain).
// User logs in in that window; we poll cookies over Chrome DevTools Protocol
// until CookieName appears, then return the session value and full Cookie header.
//
// No macOS Keychain calls. Cookies come from the live browser via CDP.
func CaptureWebAuthViaBrowser(target WebLoginTarget, timeout time.Duration) (*CapturedWebAuth, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	bin, err := findChromiumBinary()
	if err != nil {
		return nil, err
	}
	dir, err := profileDir(target.Profile)
	if err != nil {
		return nil, err
	}
	// Stale Singleton* from a prior hung login makes the new Chrome hand off
	// to a non-debug window — user logs in there, CDP never sees cookies.
	clearChromiumSingletonLocks(dir)
	// Clear previous session cookies so the user is required to log in anew
	clearTargetSessionCookies(dir)

	port, portHold, err := pickFreePort()
	if err != nil {
		return nil, err
	}

	// Release the reserved port in the last instant before the browser claims it.
	_ = portHold.Close()

	cmd := exec.Command(bin,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--new-window",
		target.StartURL,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}
	// NOTE: do not wipe cookies on the way out. Clearing on entry forces a fresh
	// login; clearing again on exit destroys the session we just captured, which
	// breaks RefreshWebAuthFromProfile (it reopens this same profile expecting
	// the cookie to still be there).
	defer terminateBrowser(cmd)

	deadline := time.Now().Add(timeout)
	var wsURL string
	for time.Now().Before(deadline) {
		wsURL, err = debuggerWSURL(port)
		if err == nil && wsURL != "" {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if wsURL == "" {
		return nil, fmt.Errorf("browser DevTools not ready on port %d: %v", port, err)
	}

	fmt.Printf("Browser opened (%s). Log in THERE (dedicated window) — waiting for %s (timeout %s)…\n",
		filepath.Base(bin), target.CookieName, timeout.Round(time.Second))
	fmt.Println("Tip: stay in the window amux opened (profile ~/.am/browser-profiles/" + target.Profile + ").")

	lastBeat := time.Now()
	for time.Now().Before(deadline) {
		cookies, gerr := cdpListCookies(wsURL)
		if gerr == nil {
			if val := pickSessionCookie(cookies, target); val != "" {
				return &CapturedWebAuth{
					SessionValue: val,
					CookieHeader: cookieHeaderForHost(cookies, target.CookieHost),
				}, nil
			}
		}
		if time.Since(lastBeat) >= 15*time.Second {
			left := time.Until(deadline).Round(time.Second)
			hint := summarizeAuthCookies(cookies, target.CookieHost)
			if gerr != nil {
				fmt.Printf("…still waiting (%s left); CDP: %v\n", left, gerr)
			} else if hint != "" {
				fmt.Printf("…still waiting (%s left); seen: %s\n", left, hint)
			} else {
				fmt.Printf("…still waiting (%s left); no auth cookie yet — finish login in the dedicated window\n", left)
			}
			lastBeat = time.Now()
		}
		time.Sleep(1200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for %s after login — stay signed in in the amux window and retry", target.CookieName)
}

// RefreshWebAuthFromProfile opens the existing ~/.am/browser-profiles/<Profile>
// window briefly and returns session + full Cookie header if already logged in.
// Does not wait for interactive login (short timeout).
func RefreshWebAuthFromProfile(target WebLoginTarget, timeout time.Duration) (*CapturedWebAuth, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	bin, err := findChromiumBinary()
	if err != nil {
		return nil, err
	}
	dir, err := profileDir(target.Profile)
	if err != nil {
		return nil, err
	}
	clearChromiumSingletonLocks(dir)

	port, portHold, err := pickFreePort()
	if err != nil {
		return nil, err
	}

	startURL := "https://" + strings.TrimPrefix(target.CookieHost, ".") + "/"
	if target.CookieHost == "claude.ai" {
		startURL = "https://claude.ai/"
	}
	// Release the reserved port in the last instant before the browser claims it.
	_ = portHold.Close()

	cmd := exec.Command(bin,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--new-window",
		startURL,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}
	defer func() {
		terminateBrowser(cmd)
	}()

	deadline := time.Now().Add(timeout)
	var wsURL string
	for time.Now().Before(deadline) {
		wsURL, err = debuggerWSURL(port)
		if err == nil && wsURL != "" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if wsURL == "" {
		return nil, fmt.Errorf("browser DevTools not ready on port %d: %v", port, err)
	}

	// Give the profile a moment to load cookies into the jar.
	time.Sleep(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		cookies, gerr := cdpListCookies(wsURL)
		if gerr == nil {
			if val := pickSessionCookie(cookies, target); val != "" {
				hdr := cookieHeaderForHost(cookies, target.CookieHost)
				if hdr == "" {
					hdr = target.CookieName + "=" + val
				}
				return &CapturedWebAuth{SessionValue: val, CookieHeader: hdr}, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("no %s in profile %s — run am login %s", target.CookieName, target.Profile, target.Name)
}

// CaptureCookieViaBrowser opens a dedicated Chromium window and returns the
// session cookie value (see CaptureWebAuthViaBrowser).
func CaptureCookieViaBrowser(target WebLoginTarget, timeout time.Duration) (cookieValue string, err error) {
	auth, err := CaptureWebAuthViaBrowser(target, timeout)
	if err != nil {
		return "", err
	}
	return auth.SessionValue, nil
}

// cookieHeaderForHost builds a Cookie request header from CDP cookies whose
// domain matches hostSubstr (e.g. "claude.ai"). Includes Cloudflare /
// ancillary cookies that sessionKey alone does not cover.
func cookieHeaderForHost(cookies []cdpCookie, hostSubstr string) string {
	var parts []string
	seen := map[string]bool{}
	for _, c := range cookies {
		if c.Name == "" || c.Value == "" {
			continue
		}
		if hostSubstr != "" {
			d := strings.TrimPrefix(c.Domain, ".")
			if !strings.Contains(d, hostSubstr) && !strings.Contains(hostSubstr, d) {
				continue
			}
		}
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// terminateBrowser shuts down the browser amux launched. It asks politely first
// so Chromium flushes its cookie jar to the profile directory; a hard Kill can
// lose the session we just captured. Safe to call when the process never started.
func terminateBrowser(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func clearChromiumSingletonLocks(dir string) {
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// pickSessionCookie returns the session cookie value for target.
// ChatGPT often splits the token across __Secure-next-auth.session-token.0/.1/.2.
func pickSessionCookie(cookies []cdpCookie, target WebLoginTarget) string {
	hostOK := func(domain string) bool {
		if target.CookieHost == "" {
			return true
		}
		d := strings.TrimPrefix(domain, ".")
		return strings.Contains(d, target.CookieHost) || strings.Contains(target.CookieHost, d)
	}

	// Exact name first.
	for _, c := range cookies {
		if !hostOK(c.Domain) {
			continue
		}
		if c.Name == target.CookieName && c.Value != "" {
			return c.Value
		}
	}

	// Chunked next-auth session token (.0 + .1 + .2 …) — must keep names;
	// concatenating raw values alone breaks /api/auth/session.
	if strings.Contains(target.CookieName, "session-token") {
		var hdr []string
		for i := 0; i < 8; i++ {
			want := fmt.Sprintf("%s.%d", target.CookieName, i)
			for _, c := range cookies {
				if !hostOK(c.Domain) {
					continue
				}
				if c.Name == want && c.Value != "" {
					hdr = append(hdr, want+"="+c.Value)
					break
				}
			}
		}
		if len(hdr) > 0 {
			return strings.Join(hdr, "; ")
		}
	}
	return ""
}

func summarizeAuthCookies(cookies []cdpCookie, hostSubstr string) string {
	var names []string
	seen := map[string]bool{}
	for _, c := range cookies {
		if hostSubstr != "" && !strings.Contains(c.Domain, hostSubstr) && !strings.Contains(c.Domain, "openai") {
			continue
		}
		n := c.Name
		if !(strings.Contains(n, "session") || strings.Contains(n, "auth") || strings.HasPrefix(n, "oai-") || strings.Contains(n, "token")) {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
		if len(names) >= 6 {
			break
		}
	}
	return strings.Join(names, ", ")
}

func profileDir(name string) (string, error) {
	dir := filepath.Join(types.BaseDir(), "browser-profiles", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// findChromiumBinary locates a Chromium-family browser that supports
// --remote-debugging-port. AMUX_BROWSER_BINARY overrides the search.
//
// Arc is deliberately excluded: it ignores --new-window/--user-data-dir in the
// way we rely on here, so CDP never observes the login window.
func findChromiumBinary() (string, error) {
	if env := strings.TrimSpace(os.Getenv("AMUX_BROWSER_BINARY")); env != "" {
		st, err := os.Stat(env)
		if err != nil || st.IsDir() {
			return "", fmt.Errorf("AMUX_BROWSER_BINARY=%q is not an executable file", env)
		}
		return env, nil
	}

	var candidates []string
	if def := detectDefaultChromiumPath(); def != "" {
		candidates = append(candidates, def)
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Volumes/Macintosh HD/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	case "windows":
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		)
	default:
		candidates = append(candidates,
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/microsoft-edge",
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chrome/Edge/Brave/Chromium found — install one, " +
		"set AMUX_BROWSER_BINARY=/path/to/browser, or paste --cookie/--token instead")
}

func detectDefaultChromiumPath() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("defaults", "read", "com.apple.LaunchServices/com.apple.launchservices.secure", "LSHandlers").Output()
	if err != nil {
		return ""
	}
	content := string(out)
	for _, block := range strings.Split(content, "{") {
		if strings.Contains(block, "LSHandlerURLScheme = http") || strings.Contains(block, "LSHandlerURLScheme = https") {
			lower := strings.ToLower(block)
			switch {
			case strings.Contains(lower, "microsoft.edgemac") || strings.Contains(lower, "msedge"):
				p := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			case strings.Contains(lower, "brave.browser"):
				p := "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			case strings.Contains(lower, "google.chrome"):
				p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			case strings.Contains(lower, "chromium"):
				p := "/Applications/Chromium.app/Contents/MacOS/Chromium"
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			}
		}
	}
	return ""
}

// pickFreePort reserves an ephemeral port and hands back both the number and
// the listener holding it. The caller must Close the listener immediately
// before launching the browser: keeping it open until then stops another
// process from claiming the port in the gap (TOCTOU), which previously showed
// up as "browser DevTools not ready on port N".
func pickFreePort() (int, net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, err
	}
	return ln.Addr().(*net.TCPAddr).Port, ln, nil
}

func debuggerWSURL(port int) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var meta struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", err
	}
	if meta.WS == "" {
		return "", fmt.Errorf("empty webSocketDebuggerUrl")
	}
	return meta.WS, nil
}

var cdpID atomic.Int64

type cdpCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

func cdpListCookies(wsURL string) ([]cdpCookie, error) {
	cookies, err := cdpCallCookies(wsURL, "Network.getAllCookies")
	if err == nil && len(cookies) > 0 {
		return cookies, nil
	}
	// Chrome ≥122 browser target often prefers Storage.getCookies.
	alt, err2 := cdpCallCookies(wsURL, "Storage.getCookies")
	if err2 == nil {
		return alt, nil
	}
	if err != nil {
		return nil, err
	}
	return cookies, err2
}

func cdpCallCookies(wsURL, method string) ([]cdpCookie, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	id := cdpID.Add(1)
	req := map[string]any{"id": id, "method": method}
	if err := conn.WriteJSON(req); err != nil {
		return nil, err
	}

	for {
		var msg struct {
			ID     int64 `json:"id"`
			Result struct {
				Cookies []cdpCookie `json:"cookies"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return nil, err
		}
		if msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, msg.Error.Message)
		}
		return msg.Result.Cookies, nil
	}
}

// clearTargetSessionCookies removes cached cookies and session data
// so the user is prompted to log in fresh rather than reusing an existing session.
func clearTargetSessionCookies(profileDir string) {
	paths := []string{
		filepath.Join(profileDir, "Default", "Network", "Cookies"),
		filepath.Join(profileDir, "Default", "Network", "Cookies-journal"),
		filepath.Join(profileDir, "Default", "Cookies"),
		filepath.Join(profileDir, "Default", "Cookies-journal"),
		filepath.Join(profileDir, "Default", "Sessions"),
		filepath.Join(profileDir, "Default", "Session Storage"),
		filepath.Join(profileDir, "Default", "Local Storage"),
		filepath.Join(profileDir, "Default", "IndexedDB"),
		filepath.Join(profileDir, "Default", "Service Worker"),
		filepath.Join(profileDir, "Default", "Cache"),
		filepath.Join(profileDir, "Default", "Code Cache"),
		filepath.Join(profileDir, "Default", "GPUCache"),
	}
	for _, p := range paths {
		_ = os.RemoveAll(p)
	}
}
