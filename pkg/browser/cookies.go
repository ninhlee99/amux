package browser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amux-accounts/pkg/auth"

	"golang.org/x/crypto/pbkdf2"
)

type BrowserInfo struct {
	Name            string
	CookiePath      string
	KeychainService string // empty = no keychain (Firefox plaintext)
}

// KnownBrowsers returns browser cookie stores. Firefox is listed first —
// its moz_cookies.value column is plaintext, so login works without
// touching macOS Keychain. Chromium browsers need Safe Storage keychain
// only to decrypt the Cookies SQLite blob.
func KnownBrowsers() []BrowserInfo {
	home, _ := os.UserHomeDir()
	out := []BrowserInfo{}
	ffGlob := filepath.Join(home, "Library/Application Support/Firefox/Profiles/*/cookies.sqlite")
	if matches, _ := filepath.Glob(ffGlob); len(matches) > 0 {
		for _, m := range matches {
			out = append(out, BrowserInfo{Name: "Firefox", CookiePath: m})
		}
	}

	chromiumDefs := []struct {
		name     string
		baseDir  string
		keychain string
	}{
		{"Microsoft Edge", "Library/Application Support/Microsoft Edge", "Microsoft Edge Safe Storage"},
		{"Google Chrome", "Library/Application Support/Google/Chrome", "Chrome Safe Storage"},
		{"Arc", "Library/Application Support/Arc/User Data", "Arc Safe Storage"},
		{"Brave", "Library/Application Support/BraveSoftware/Brave-Browser", "Brave Safe Storage"},
	}

	for _, c := range chromiumDefs {
		base := filepath.Join(home, c.baseDir)
		candidates := []string{
			filepath.Join(base, "Default", "Network", "Cookies"),
			filepath.Join(base, "Default", "Cookies"),
		}
		if profMatches, _ := filepath.Glob(filepath.Join(base, "Profile *")); len(profMatches) > 0 {
			for _, pm := range profMatches {
				candidates = append(candidates,
					filepath.Join(pm, "Network", "Cookies"),
					filepath.Join(pm, "Cookies"),
				)
			}
		}

		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				out = append(out, BrowserInfo{
					Name:            c.name,
					CookiePath:      cand,
					KeychainService: c.keychain,
				})
			}
		}
	}
	return out
}

// ExtractCookie finds cookieName for domainFilter across every known browser
// profile. When several profiles hold a session (e.g. two ChatGPT accounts in
// Edge "Default" and "Profile 1"), the most recently used, unexpired one wins —
// that is the account the user is actively on. Only the chosen Chromium
// profile is decrypted, so at most one Safe Storage keychain read per browser.
func ExtractCookie(domainFilter, cookieName string) (string, string, error) {
	var lastErr error
	keychainFree := auth.UsesFileSecretStore()
	var cands []cookieCandidate
	for _, b := range KnownBrowsers() {
		if _, err := os.Stat(b.CookiePath); err != nil {
			continue
		}
		if keychainFree && b.KeychainService != "" {
			// Chromium cookie DBs are encrypted with a key held in the OS
			// keychain ("<Browser> Safe Storage"); keychain-free mode skips them.
			continue
		}
		got, err := listCookieCandidates(b, domainFilter, cookieName)
		if err != nil {
			lastErr = err
			continue
		}
		cands = append(cands, got...)
	}
	sortCookieCandidates(cands)
	keys := map[string][]byte{}
	for _, c := range cands {
		val, err := c.value(cookieName, keys)
		if err != nil {
			lastErr = err
			continue
		}
		if val != "" {
			return val, c.browser.Name, nil
		}
	}
	// amux's own login profile (opened by `amux login <web>`) is read over
	// DevTools: no keychain and no system-browser cookie decryption.
	if t, ok := loginTargetForDomain(domainFilter); ok && ProfileExists(t.Profile) {
		if got, err := RefreshWebAuthFromProfile(t, 20*time.Second); err == nil && got != nil {
			if v := ParseCookieHeader(got.CookieHeader, cookieName); v != "" {
				return v, "amux profile " + t.Profile, nil
			}
		} else if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("cookie %q for %q not found: %w", cookieName, domainFilter, lastErr)
	}
	return "", "", fmt.Errorf("cookie %q for %q not found in any browser", cookieName, domainFilter)
}

// loginTargetForDomain maps a cookie domain to the amux login profile for it.
func loginTargetForDomain(domain string) (WebLoginTarget, bool) {
	d := strings.TrimPrefix(strings.ToLower(domain), ".")
	for _, t := range []WebLoginTarget{ChatGPTWebLogin, ClaudeWebLogin, GeminiWebLogin} {
		if d == t.CookieHost || strings.HasSuffix(d, "."+t.CookieHost) {
			return t, true
		}
	}
	return WebLoginTarget{}, false
}

// ParseCookieHeader pulls one named cookie out of a raw Cookie header
// ("a=1; b=2") or a single "name=value" paste from DevTools.
func ParseCookieHeader(raw, cookieName string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimPrefix(raw, "Cookie:")
	raw = strings.TrimSpace(raw)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, cookieName+"=") {
			return strings.TrimPrefix(part, cookieName+"=")
		}
	}
	// Bare value paste (no "name=" prefix). Cookie values are frequently
	// base64-ish and carry '=' padding, so a bare value cannot be rejected on
	// the presence of '=' alone — only on it looking like a name=value pair.
	if !strings.Contains(raw, ";") && !looksLikeNamedCookie(raw) {
		return raw
	}
	return ""
}

// chromiumEpochOffsetMicros converts Chromium timestamps (µs since 1601) to
// Unix µs.
const chromiumEpochOffsetMicros = 11644473600000000

// cookieCandidate is one browser profile's copy of a cookie for one host,
// possibly split into next-auth chunks (name.0, name.1, …). Chromium values
// stay encrypted (hex) until the candidate is chosen.
type cookieCandidate struct {
	browser    BrowserInfo
	host       string
	lastAccess int64 // Unix µs
	rows       []cookieRow
}

type cookieRow struct {
	name  string
	value string
}

// sortCookieCandidates orders candidates most recently used first.
func sortCookieCandidates(cands []cookieCandidate) {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].lastAccess > cands[j].lastAccess })
}

// listCookieCandidates reads unexpired rows for cookieName (and its chunks)
// without decrypting anything, grouped by host.
func listCookieCandidates(b BrowserInfo, domainFilter, cookieName string) ([]cookieCandidate, error) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("am_cookie_%d.db", time.Now().UnixNano()))
	defer os.Remove(tmp)
	data, err := os.ReadFile(b.CookiePath)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return nil, err
	}
	now := time.Now()
	var query string
	if b.KeychainService == "" {
		// Firefox: plaintext value, lastAccessed in Unix µs; expiry in s (or ms
		// on newer builds — both compare correctly against now in seconds).
		query = fmt.Sprintf(
			`SELECT host, name, lastAccessed, value FROM moz_cookies WHERE host LIKE '%%%s%%' AND name LIKE '%s%%' AND (expiry = 0 OR expiry > %d);`,
			escapeSQLLike(domainFilter), escapeSQLLike(cookieName), now.Unix(),
		)
	} else {
		query = fmt.Sprintf(
			`SELECT host_key, name, last_access_utc - %d, hex(encrypted_value) FROM cookies WHERE host_key LIKE '%%%s%%' AND name LIKE '%s%%' AND (expires_utc = 0 OR expires_utc > %d);`,
			chromiumEpochOffsetMicros, escapeSQLLike(domainFilter), escapeSQLLike(cookieName), now.UnixMicro()+chromiumEpochOffsetMicros,
		)
	}
	sqlOut, err := exec.Command("/usr/bin/sqlite3", tmp, query).Output()
	if err != nil {
		return nil, fmt.Errorf("%s sqlite3: %w", b.Name, err)
	}
	return parseCookieCandidates(b, cookieName, string(sqlOut)), nil
}

// parseCookieCandidates groups sqlite3 "host|name|lastAccess|value" lines by
// host, keeping only cookieName itself and its numbered chunks.
func parseCookieCandidates(b BrowserInfo, cookieName, sqlOut string) []cookieCandidate {
	byHost := map[string]*cookieCandidate{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(sqlOut), "\n") {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 || parts[3] == "" {
			continue
		}
		host, name, value := parts[0], parts[1], parts[3]
		if name != cookieName && !strings.HasPrefix(name, cookieName+".") {
			continue
		}
		var access int64
		fmt.Sscanf(parts[2], "%d", &access)
		c, ok := byHost[host]
		if !ok {
			c = &cookieCandidate{browser: b, host: host}
			byHost[host] = c
			order = append(order, host)
		}
		if access > c.lastAccess {
			c.lastAccess = access
		}
		c.rows = append(c.rows, cookieRow{name: name, value: value})
	}
	out := make([]cookieCandidate, 0, len(order))
	for _, h := range order {
		out = append(out, *byHost[h])
	}
	return out
}

// value decrypts (Chromium) and reassembles the candidate's cookie. keys
// caches Safe Storage keys per keychain service.
func (c cookieCandidate) value(cookieName string, keys map[string][]byte) (string, error) {
	decode := func(v string) string { return v }
	if c.browser.KeychainService != "" {
		key, ok := keys[c.browser.KeychainService]
		if !ok {
			out, err := exec.Command("security", "find-generic-password", "-s", c.browser.KeychainService, "-w").Output()
			if err != nil || strings.TrimSpace(string(out)) == "" {
				return "", fmt.Errorf("%s Safe Storage unavailable (grant keychain access or paste cookie instead)", c.browser.Name)
			}
			key = pbkdf2.Key([]byte(strings.TrimSpace(string(out))), []byte("saltysalt"), 1003, 16, sha1.New)
			keys[c.browser.KeychainService] = key
		}
		decode = func(h string) string {
			enc, err := hex.DecodeString(h)
			if err != nil {
				return ""
			}
			return decryptCookie(key, enc)
		}
	}
	type chunk struct {
		idx int
		val string
	}
	var chunks []chunk
	for _, r := range c.rows {
		v := decode(r.value)
		if v == "" {
			continue
		}
		if r.name == cookieName {
			return v, nil
		}
		idx := len(c.rows) + len(chunks)
		fmt.Sscanf(strings.TrimPrefix(r.name, cookieName+"."), "%d", &idx)
		chunks = append(chunks, chunk{idx: idx, val: v})
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("%s: cookie %s not found", c.browser.Name, cookieName)
	}
	sort.SliceStable(chunks, func(i, j int) bool { return chunks[i].idx < chunks[j].idx })
	var combined strings.Builder
	for _, ch := range chunks {
		combined.WriteString(ch.val)
	}
	return combined.String(), nil
}

func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, `'`, `''`)
	return s
}

func decryptCookie(key, encVal []byte) string {
	if len(encVal) < 3 || string(encVal[:3]) != "v10" {
		return string(encVal)
	}
	data := encVal[3:]
	if len(data)%aes.BlockSize != 0 {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	iv := bytes.Repeat([]byte(" "), aes.BlockSize)
	mode := cipher.NewCBCDecrypter(block, iv)
	dec := make([]byte, len(data))
	mode.CryptBlocks(dec, data)
	if len(dec) == 0 {
		return ""
	}
	pad := int(dec[len(dec)-1])
	if pad > 0 && pad <= aes.BlockSize && len(dec) >= pad {
		dec = dec[:len(dec)-pad]
	}
	// macOS Chromium OSCrypt prepends a 32-byte signature/digest to the
	// plaintext before AES-128-CBC encryption. Strip these 32 prefix bytes.
	if len(dec) > 32 {
		dec = dec[32:]
	}
	return string(dec)
}

// ChatGPTSession is the useful subset of GET /api/auth/session.
type ChatGPTSession struct {
	AccessToken  string
	RefreshToken string
	Expires      string
	Email        string
}

// FetchChatGPTSession exchanges a next-auth session-token cookie (or any
// Cookie header that authenticates chatgpt.com) for access/refresh tokens
// from the web session endpoint — no keychain involved.
func FetchChatGPTSession(sessionTokenOrCookie string) (*ChatGPTSession, error) {
	cookie := strings.TrimSpace(sessionTokenOrCookie)
	if !looksLikeNamedCookie(cookie) {
		cookie = "__Secure-next-auth.session-token=" + cookie
	}

	req, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/api/auth/session", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session endpoint status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		Expires      string `json:"expires"`
		Error        string `json:"error"`
		User         struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if data.AccessToken == "" {
		return nil, fmt.Errorf("no accessToken in session response: %s", data.Error)
	}
	return &ChatGPTSession{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		Expires:      data.Expires,
		Email:        data.User.Email,
	}, nil
}

// looksLikeNamedCookie reports whether raw appears to be a "name=value" pair
// rather than a bare cookie value. Only a plausible cookie-name before the
// first '=' counts; trailing base64 padding ("abc==") does not.
func looksLikeNamedCookie(raw string) bool {
	i := strings.Index(raw, "=")
	if i <= 0 {
		return false
	}
	name := raw[:i]
	for _, r := range name {
		isAllowed := r == '-' || r == '_' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAllowed {
			return false
		}
	}
	// A real value must follow the '='. Trailing base64 padding ("abc=",
	// "abc==") is not a value, so those stay bare pastes.
	return strings.Trim(strings.TrimSpace(raw[i+1:]), "=") != ""
}
