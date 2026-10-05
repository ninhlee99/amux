package muse

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

// maxAttachmentBytes bounds remote attachments fetched on the caller's behalf.
const maxAttachmentBytes = 100 << 20

// materializeFiles turns attachment specs into local paths the browser can
// read: plain paths and file:// URLs are used as-is, http(s):// and data:
// URLs are written to a temp dir that cleanup removes.
func materializeFiles(ctx context.Context, specs []string) (paths []string, cleanup func(), err error) {
	var tmp string
	cleanup = func() {
		if tmp != "" {
			_ = os.RemoveAll(tmp)
		}
	}
	tempFile := func(name string) (string, error) {
		if tmp == "" {
			d, err := os.MkdirTemp("", "amux-muse-")
			if err != nil {
				return "", err
			}
			tmp = d
		}
		// One subdirectory per attachment keeps the original file name (what
		// Muse shows and reasons about) while avoiding collisions.
		sub := filepath.Join(tmp, fmt.Sprint(len(paths)))
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return "", err
		}
		return filepath.Join(sub, sanitizeName(name)), nil
	}

	for _, raw := range specs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		lower := strings.ToLower(s)
		switch {
		case strings.HasPrefix(lower, "data:"):
			data, mt, derr := decodeDataURL(s)
			if derr != nil {
				return nil, cleanup, derr
			}
			dst, terr := tempFile("attachment" + extForMIME(mt))
			if terr != nil {
				return nil, cleanup, terr
			}
			if werr := os.WriteFile(dst, data, 0o600); werr != nil {
				return nil, cleanup, werr
			}
			paths = append(paths, dst)
		case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
			name := "attachment"
			if u, perr := url.Parse(s); perr == nil && path.Base(u.Path) != "/" && path.Base(u.Path) != "." {
				name = path.Base(u.Path)
			}
			dst, terr := tempFile(name)
			if terr != nil {
				return nil, cleanup, terr
			}
			if ferr := downloadTo(ctx, s, dst); ferr != nil {
				return nil, cleanup, ferr
			}
			paths = append(paths, dst)
		case strings.HasPrefix(lower, "file://"):
			u, perr := url.Parse(s)
			if perr != nil {
				return nil, cleanup, perr
			}
			paths = append(paths, u.Path)
		default:
			abs, aerr := filepath.Abs(s)
			if aerr != nil {
				return nil, cleanup, aerr
			}
			if _, serr := os.Stat(abs); serr != nil {
				return nil, cleanup, fmt.Errorf("muse: attachment %s: %w", s, serr)
			}
			paths = append(paths, abs)
		}
	}
	return paths, cleanup, nil
}

func decodeDataURL(s string) ([]byte, string, error) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return nil, "", errors.New("muse: malformed data: URL")
	}
	meta, payload := s[len("data:"):comma], s[comma+1:]
	mt := strings.Split(meta, ";")[0]
	if strings.HasSuffix(meta, ";base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", fmt.Errorf("muse: data: URL: %w", err)
		}
		return b, mt, nil
	}
	dec, err := url.PathUnescape(payload)
	if err != nil {
		return nil, "", err
	}
	return []byte(dec), mt, nil
}

func downloadTo(ctx context.Context, src, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("muse: fetch %s: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("muse: fetch %s: HTTP %d", src, resp.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxAttachmentBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxAttachmentBytes {
		err = fmt.Errorf("muse: %s exceeds %d MB", src, maxAttachmentBytes>>20)
	}
	return err
}

func sanitizeName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." {
		return "file"
	}
	return name
}

var preferredExt = map[string]string{
	"text/plain": ".txt", "text/markdown": ".md", "text/csv": ".csv", "application/json": ".json",
	"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif",
	"image/webp": ".webp", "video/mp4": ".mp4", "video/webm": ".webm", "audio/mpeg": ".mp3",
}

func extForMIME(mt string) string {
	mt = strings.ToLower(strings.TrimSpace(strings.Split(mt, ";")[0]))
	if e, ok := preferredExt[mt]; ok {
		return e
	}
	if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
		return exts[0]
	}
	switch {
	case strings.Contains(mt, "mp4"):
		return ".mp4"
	case strings.Contains(mt, "png"):
		return ".png"
	case strings.Contains(mt, "jpeg"), strings.Contains(mt, "jpg"):
		return ".jpg"
	case strings.Contains(mt, "webp"):
		return ".webp"
	case strings.Contains(mt, "gif"):
		return ".gif"
	case strings.Contains(mt, "webm"):
		return ".webm"
	}
	return ".bin"
}

// MediaItem lists the media links found in one message.
type MediaItem struct {
	Index int      `json:"index"`
	Role  string   `json:"role"`
	Text  string   `json:"text"`
	URLs  []string `json:"urls"`
}

// SavedMedia is one downloaded file (or the error for it).
type SavedMedia struct {
	URL   string `json:"url"`
	File  string `json:"file,omitempty"`
	Bytes int    `json:"bytes,omitempty"`
	MIME  string `json:"mime,omitempty"`
	Error string `json:"error,omitempty"`
}

// MediaResult is the outcome of Media.
type MediaResult struct {
	Items     []MediaItem  `json:"items"`
	URLs      []string     `json:"urls"`
	ThreadURL string       `json:"threadUrl"`
	Dir       string       `json:"dir,omitempty"`
	Saved     []SavedMedia `json:"saved,omitempty"`
}

// DefaultMediaDir is where downloads land when no dir is given.
func DefaultMediaDir() string {
	return filepath.Join(types.BaseDir(), "muse", "media")
}

// Media extracts image/video/attachment links from a chat; with download the
// files are fetched from inside the page (handles blob: and cookie-authed
// URLs) into dir. Muse share links expire after ~2 days.
func (d *Driver) Media(ctx context.Context, target string, download bool, dir string) (*MediaResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.requirePage(ctx)
	if err != nil {
		return nil, err
	}
	if target != "" {
		if err := d.openChat(ctx, p, target); err != nil {
			return nil, err
		}
	} else if err := d.gotoApp(ctx, p); err != nil {
		return nil, err
	}
	var items []MediaItem
	err = p.Eval(ctx, fmt.Sprintf(`[...document.querySelectorAll(%s)].map((el, index) => {
  const urls = [...new Set([...el.querySelectorAll('a[href], img[src], video[src], source[src]')]
    .map(n => n.href || n.currentSrc || n.src || n.getAttribute('src')).filter(u => u && /^(https?:|blob:)/i.test(u)))];
  const text = (el.innerText || '').replace(/^(You:|User message:|Assistant message:)\s*/i, '').replace(/\s+/g, ' ').trim().slice(0, 120);
  return {index, role: el.getAttribute('data-message-role') || '', text, urls};
}).filter(m => m.urls.length)`, js(Selectors.Message)), &items)
	if err != nil {
		return nil, err
	}
	res := &MediaResult{Items: items, ThreadURL: d.url(ctx, p)}
	seen := map[string]bool{}
	for _, it := range items {
		for _, u := range it.URLs {
			if !seen[u] {
				seen[u] = true
				res.URLs = append(res.URLs, u)
			}
		}
	}
	if !download || len(res.URLs) == 0 {
		return res, nil
	}
	if dir == "" {
		dir = DefaultMediaDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	res.Dir = dir
	for i, u := range res.URLs {
		res.Saved = append(res.Saved, d.saveOne(ctx, p, u, dir, i))
	}
	return res, nil
}

func (d *Driver) saveOne(ctx context.Context, p Page, u, dir string, i int) SavedMedia {
	label := u
	if strings.HasPrefix(u, "blob:") {
		label = "blob"
	}
	var r struct {
		OK    bool   `json:"ok"`
		MIME  string `json:"mime"`
		B64   string `json:"b64"`
		Error string `json:"error"`
	}
	err := p.Eval(ctx, fmt.Sprintf(`(async () => {
  try {
    const res = await fetch(%s); const bytes = new Uint8Array(await res.arrayBuffer());
    let s = ''; for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    return {ok: res.ok, mime: res.headers.get('content-type') || '', b64: btoa(s)};
  } catch (e) { return {ok: false, error: String(e)}; }
})()`, js(u)), &r)
	if err != nil {
		return SavedMedia{URL: label, Error: err.Error()}
	}
	if !r.OK {
		if r.Error == "" {
			r.Error = "fetch failed"
		}
		return SavedMedia{URL: label, Error: r.Error}
	}
	data, err := base64.StdEncoding.DecodeString(r.B64)
	if err != nil {
		return SavedMedia{URL: label, Error: err.Error()}
	}
	name := "media"
	if pu, perr := url.Parse(u); perr == nil && !strings.HasPrefix(u, "blob:") {
		if b := path.Base(pu.Path); b != "" && b != "/" && b != "." {
			name = b
		}
	}
	if filepath.Ext(name) == "" {
		name += extForMIME(strings.Split(r.MIME, ";")[0])
	}
	dst := filepath.Join(dir, fmt.Sprintf("%s-%02d-%s", time.Now().Format("20060102-150405"), i, sanitizeName(name)))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return SavedMedia{URL: label, Error: err.Error()}
	}
	return SavedMedia{URL: label, File: dst, Bytes: len(data), MIME: r.MIME}
}
