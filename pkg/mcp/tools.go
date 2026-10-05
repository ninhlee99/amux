package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"amux-accounts/pkg/muse"
)

// ProviderInfo is one pool account as shown to MCP clients (no secrets).
type ProviderInfo struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Tier       string `json:"tier"`
	Priority   int    `json:"priority"`
	InPool     bool   `json:"inPool"`
	Configured bool   `json:"configured"`
	Account    string `json:"account,omitempty"`
	Model      string `json:"model,omitempty"`
}

// AskRequest is a one-shot question routed through amux's account pool.
type AskRequest struct {
	Prompt   string `json:"prompt"`
	System   string `json:"system,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Timeout  time.Duration
}

// AskResult is the answer and which account produced it.
type AskResult struct {
	Provider  string `json:"provider"`
	Text      string `json:"text"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Backend is what the amux_* tools need from the rest of amux.
type Backend interface {
	Providers() ([]ProviderInfo, error)
	Ask(ctx context.Context, req AskRequest, onDelta func(string)) (*AskResult, error)
	Status(ctx context.Context) (map[string]any, error)
}

// MuseClient is the subset of *muse.Driver the muse_* tools call.
type MuseClient interface {
	Status(ctx context.Context) muse.Status
	Login(ctx context.Context, timeout time.Duration) (muse.AuthState, error)
	NewChat(ctx context.Context) (string, error)
	OpenChat(ctx context.Context, target string) (string, error)
	Chat(ctx context.Context, prompt string, o muse.ChatOptions) (*muse.ChatResult, error)
	ReadChat(ctx context.Context, target string, max int) ([]muse.Message, string, error)
	ListChats(ctx context.Context, query string) ([]muse.ChatInfo, error)
	Media(ctx context.Context, target string, download bool, dir string) (*muse.MediaResult, error)
	DumpDOM(ctx context.Context, maxChars int) (*muse.DOMDump, error)
	Close()
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func seconds(n int, def time.Duration) time.Duration {
	if n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}

// RegisterAmuxTools adds the provider-agnostic tools backed by the pool.
func RegisterAmuxTools(s *Server, b Backend) {
	s.Register(Tool{
		Name:        "amux_providers",
		Title:       "List chat providers",
		Description: "List the AI accounts amux can route to (subscriptions, web chats such as ChatGPT/Claude/Gemini/Muse, and API keys), with tier, priority and whether each is in the rotation pool. Use an id with amux_ask's provider argument.",
		InputSchema: obj(map[string]any{}),
		ReadOnly:    true,
		Handler: func(context.Context, json.RawMessage, func(string)) (any, error) {
			ps, err := b.Providers()
			if err != nil {
				return nil, err
			}
			return map[string]any{"providers": ps, "count": len(ps)}, nil
		},
	})
	s.Register(Tool{
		Name:  "amux_ask",
		Title: "Ask another AI",
		Description: "Send a self-contained prompt to another AI through amux's account pool and return its answer. " +
			"Choose where it goes with provider (see amux_providers): an exact account id such as \"gemini:web:01\" pins that account; " +
			"a family such as \"gemini:web\", \"chatgpt\" or \"muse:web\" lets amux pick among those accounts with failover; " +
			"omit it to let amux pick from the whole pool. Good for second opinions, research and drafting. " +
			"The other AI cannot see your files or conversation — include all context it needs.",
		InputSchema: obj(map[string]any{
			"prompt":      str("The full, self-contained question or task."),
			"system":      str("Optional system instructions."),
			"provider":    str("Optional exact account id (pins it) or account family such as \"gemini:web\" (amux picks within it)."),
			"model":       str("Optional model override for the chosen account."),
			"timeout_sec": num("Give up after this many seconds (default 300)."),
		}, "prompt"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Prompt, System, Provider, Model string
				TimeoutSec                      int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Prompt) == "" {
				return nil, errors.New("prompt is required")
			}
			req := AskRequest{Prompt: a.Prompt, System: a.System, Provider: a.Provider, Model: a.Model, Timeout: seconds(a.TimeoutSec, 5*time.Minute)}
			ctx, cancel := context.WithTimeout(ctx, req.Timeout)
			defer cancel()
			return b.Ask(ctx, req, throttle(progress))
		},
	})
	s.Register(Tool{
		Name:        "amux_status",
		Title:       "amux status",
		Description: "Report whether the amux gateway is running, where coding agents should point (base URLs), and how many accounts are usable.",
		InputSchema: obj(map[string]any{}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, _ json.RawMessage, _ func(string)) (any, error) {
			return b.Status(ctx)
		},
	})
}

// RegisterMuseTools adds the Meta Muse tools. client is resolved lazily so
// the browser only starts when a muse_* tool is actually used.
func RegisterMuseTools(s *Server, client func() MuseClient) {
	s.Register(Tool{
		Name:        "muse_status",
		Title:       "Muse status",
		Description: "Start (or attach to) the Muse browser if needed and report login and composer state.",
		InputSchema: obj(map[string]any{}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, _ json.RawMessage, _ func(string)) (any, error) {
			return client().Status(ctx), nil
		},
	})
	s.Register(Tool{
		Name:        "muse_login",
		Title:       "Muse login",
		Description: "Open muse.ai in amux's dedicated browser profile and wait for the user to finish signing in with their Meta account (the window is visible).",
		InputSchema: obj(map[string]any{"timeout_sec": num("How long to wait for sign-in (default 300).")}),
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				TimeoutSec int `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			st, err := client().Login(ctx, seconds(a.TimeoutSec, 5*time.Minute))
			if err != nil {
				return nil, err
			}
			return map[string]any{"loggedIn": st.OK, "viewerId": st.ViewerID}, nil
		},
	})
	s.Register(Tool{
		Name:        "muse_new_chat",
		Title:       "Muse new chat",
		Description: "Start a fresh Muse side chat and return its URL.",
		InputSchema: obj(map[string]any{}),
		Handler: func(ctx context.Context, _ json.RawMessage, _ func(string)) (any, error) {
			u, err := client().NewChat(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"threadUrl": u}, nil
		},
	})
	s.Register(Tool{
		Name:  "muse_chat",
		Title: "Chat with Muse",
		Description: "Send a prompt to Meta Muse and wait for the full reply. Muse can also generate images and video (it replies with links; use muse_media to download). " +
			"Attach images/video/documents with files (absolute paths, file://, http(s):// or data: URLs). " +
			"Target an existing chat with chat (title, sidebar index, thread URL or id) or start fresh with new_thread.",
		InputSchema: obj(map[string]any{
			"prompt":      str("Message to send (sent verbatim)."),
			"files":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Attachments."},
			"chat":        str("Chat to send in (title, index, thread URL or id). Default: current chat."),
			"new_thread":  boolean("Start a new side chat first."),
			"timeout_sec": num("Max seconds to wait for the reply (default 240)."),
		}, "prompt"),
		Handler: func(ctx context.Context, raw json.RawMessage, progress func(string)) (any, error) {
			var a struct {
				Prompt     string   `json:"prompt"`
				Files      []string `json:"files"`
				Chat       string   `json:"chat"`
				NewThread  bool     `json:"new_thread"`
				TimeoutSec int      `json:"timeout_sec"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return client().Chat(ctx, a.Prompt, muse.ChatOptions{
				Timeout: seconds(a.TimeoutSec, 4*time.Minute), NewThread: a.NewThread, Chat: a.Chat,
				Files: a.Files, OnDelta: throttle(progress),
			})
		},
	})
	s.Register(Tool{
		Name:        "muse_read_last",
		Title:       "Muse last reply",
		Description: "Return the latest assistant message of a chat (current chat by default) without sending anything.",
		InputSchema: obj(map[string]any{"chat": str("Optional chat (title, index, thread URL or id).")}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				Chat string `json:"chat"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			msgs, u, err := client().ReadChat(ctx, a.Chat, 0)
			if err != nil {
				return nil, err
			}
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == "assistant" {
					return map[string]any{"reply": msgs[i].Text, "media": msgs[i].Media, "threadUrl": u}, nil
				}
			}
			return map[string]any{"reply": "", "threadUrl": u}, nil
		},
	})
	s.Register(Tool{
		Name:        "muse_chats",
		Title:       "List Muse chats",
		Description: "List Muse chats from the sidebar (Main chat, Channels, Side chats), optionally filtered by title.",
		InputSchema: obj(map[string]any{"query": str("Case-insensitive title filter.")}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				Query string `json:"query"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			chats, err := client().ListChats(ctx, a.Query)
			if err != nil {
				return nil, err
			}
			return map[string]any{"chats": chats}, nil
		},
	})
	s.Register(Tool{
		Name:        "muse_open_chat",
		Title:       "Open Muse chat",
		Description: "Open a Muse chat by title, sidebar index, thread URL or thread id (empty = main chat).",
		InputSchema: obj(map[string]any{"target": str("Chat to open.")}, "target"),
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				Target string `json:"target"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			u, err := client().OpenChat(ctx, a.Target)
			if err != nil {
				return nil, err
			}
			return map[string]any{"threadUrl": u}, nil
		},
	})
	s.Register(Tool{
		Name:        "muse_read_chat",
		Title:       "Read Muse chat",
		Description: "Read the messages (all roles, with media links) of a Muse chat.",
		InputSchema: obj(map[string]any{
			"chat": str("Optional chat (title, index, thread URL or id). Default: current chat."),
			"max":  num("Return at most this many most-recent messages (default 100)."),
		}),
		ReadOnly: true,
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				Chat string `json:"chat"`
				Max  int    `json:"max"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if a.Max <= 0 {
				a.Max = 100
			}
			msgs, u, err := client().ReadChat(ctx, a.Chat, a.Max)
			if err != nil {
				return nil, err
			}
			return map[string]any{"messages": msgs, "count": len(msgs), "threadUrl": u}, nil
		},
	})
	s.Register(Tool{
		Name:        "muse_media",
		Title:       "Muse media",
		Description: "List image/video/attachment links in a Muse chat; with download=true save them locally (Muse links expire after about 2 days).",
		InputSchema: obj(map[string]any{
			"chat":     str("Optional chat (title, index, thread URL or id)."),
			"download": boolean("Download the files."),
			"dir":      str("Download directory (default ~/.amux/muse/media)."),
		}),
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				Chat     string `json:"chat"`
				Download bool   `json:"download"`
				Dir      string `json:"dir"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return client().Media(ctx, a.Chat, a.Download, a.Dir)
		},
	})
	s.Register(Tool{
		Name:        "muse_dump_dom",
		Title:       "Muse DOM dump",
		Description: "Diagnostics: element counts and transcript HTML, for re-verifying selectors when Muse changes its UI.",
		InputSchema: obj(map[string]any{"max_chars": num("Truncate HTML to this many characters (default 20000).")}),
		ReadOnly:    true,
		Handler: func(ctx context.Context, raw json.RawMessage, _ func(string)) (any, error) {
			var a struct {
				MaxChars int `json:"max_chars"`
			}
			if err := DecodeArgs(raw, &a); err != nil {
				return nil, err
			}
			return client().DumpDOM(ctx, a.MaxChars)
		},
	})
	s.Register(Tool{
		Name:        "muse_close",
		Title:       "Close Muse browser",
		Description: "Close the Muse tab. A browser amux launched is shut down; one it attached to is only disconnected.",
		InputSchema: obj(map[string]any{}),
		Handler: func(context.Context, json.RawMessage, func(string)) (any, error) {
			client().Close()
			return map[string]any{"ok": true}, nil
		},
	})
}

// throttle turns cumulative-text callbacks into at most ~1 progress message
// per second carrying the newest tail of the text.
func throttle(progress func(string)) func(string) {
	var last time.Time
	return func(full string) {
		if time.Since(last) < time.Second {
			return
		}
		last = time.Now()
		tail := full
		if r := []rune(tail); len(r) > 200 {
			tail = "…" + string(r[len(r)-200:])
		}
		progress(fmt.Sprintf("%d chars: %s", len(full), tail))
	}
}
