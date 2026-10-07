package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// PoolBackend serves the amux_* tools from accounts.json in-process, with the
// same AccountPoolRouter (failover, guard pacing, quarantine) the gateway uses.
type PoolBackend struct {
	AccountsPath string

	once sync.Once
	pool *router.AccountPoolRouter
	err  error
}

func (b *PoolBackend) path() string {
	if b.AccountsPath != "" {
		return b.AccountsPath
	}
	return provider.DefaultAccountsPath()
}

func (b *PoolBackend) router() (*router.AccountPoolRouter, error) {
	b.once.Do(func() {
		adapters, err := provider.LoadAccounts(b.path())
		if err != nil && len(adapters) == 0 {
			b.err = err
			return
		}
		b.pool = router.NewAccountPoolRouter(adapters)
		b.pool.SetAutoRotateFilter(identity.AutoRotateFilter(""))
		// Subscriptions (often the very agent calling this tool) are picked
		// automatically only after `amux pool add`; otherwise pin them.
		b.pool.SetSubscriptionPoolFilter(identity.PoolMemberFilter(""))
		if all, err := provider.LoadAllAddressable(b.path()); err == nil {
			b.pool.SetDirectory(all)
		}
	})
	return b.pool, b.err
}

// Providers lists accounts.json rows without secrets.
func (b *PoolBackend) Providers() ([]ProviderInfo, error) {
	f, err := provider.LoadConfigFile(b.path())
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w (add one with `amux login`)", err)
	}
	inPool := identity.PoolMemberFilter("")
	out := make([]ProviderInfo, 0, len(f.Providers))
	for _, p := range f.Providers {
		acct := p.ToAccount()
		pooled := p.InRotatePool()
		if acct.Type == types.AccountTypeSubscription {
			pooled = pooled && inPool(p.ID)
		}
		out = append(out, ProviderInfo{
			ID: p.ID, Type: p.Type, Tier: string(acct.Type), Priority: p.Priority,
			InPool: pooled, Configured: p.HasCredentials(),
			Account: strings.TrimPrefix(acct.Email, "-"), Model: p.Model,
		})
	}
	return out, nil
}

// Ask routes one prompt through the pool (or a pinned account) and collects
// the streamed answer.
func (b *PoolBackend) Ask(ctx context.Context, a AskRequest, onDelta func(string)) (*AskResult, error) {
	pool, err := b.router()
	if err != nil {
		return nil, err
	}
	var msgs []types.ChatMessage
	if strings.TrimSpace(a.System) != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: a.System})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: a.Prompt})
	req := &types.ChatRequest{
		Model:         a.Model,
		Messages:      msgs,
		Stream:        true,
		FullContext:   true, // one-shot: never continue a stale web thread
		ClientDialect: "openai",
	}
	start := time.Now()
	var ch <-chan types.StreamChunk
	if p := strings.TrimSpace(a.Provider); p != "" {
		if id, merr := provider.MatchID(b.path(), p); merr == nil && strings.EqualFold(id, p) {
			ch, err = pool.SendNamed(ctx, id, req) // exact account
		} else {
			ch, err = pool.SendProvider(ctx, p, req) // family: amux picks + fails over
		}
	} else {
		ch, err = pool.Send(ctx, req)
	}
	if err != nil {
		if errors.Is(err, types.ErrAuthentication) {
			return nil, fmt.Errorf("%w (run 'amux login' to refresh credentials)", err)
		}
		return nil, err
	}
	var sb strings.Builder
	var thinking strings.Builder
	var recordedToolCalls []types.ToolCall
	served := req.ServingAccount
	for c := range ch {
		if c.Error != nil {
			if sb.Len() == 0 {
				if errors.Is(c.Error, types.ErrAuthentication) {
					return nil, fmt.Errorf("%w (run 'amux login' to refresh credentials)", c.Error)
				}
				return nil, c.Error
			}
			break
		}
		if c.ID != "" && served == "" {
			served = c.ID
		}
		if c.Thinking != "" {
			thinking.WriteString(c.Thinking)
			if onDelta != nil && sb.Len() == 0 {
				onDelta("🧠 Thinking...")
			}
		}
		if c.Content != "" {
			sb.WriteString(c.Content)
			if onDelta != nil {
				onDelta(sb.String())
			}
		}
		for _, tc := range c.ToolCalls {
			recordedToolCalls = append(recordedToolCalls, tc)
			fmt.Fprintf(&sb, "\n[tool_call %s %s]", tc.Name, tc.Arguments)
		}
	}
	if err := ctx.Err(); err != nil && sb.Len() == 0 {
		return nil, err
	}
	text := strings.TrimSpace(sb.String())
	clean := strings.TrimSpace(tools.StripInternalThoughtAndToolTags(text))
	if len(recordedToolCalls) > 0 {
		var parts []string
		for _, tc := range recordedToolCalls {
			parts = append(parts, fmt.Sprintf("%s(%s)", tc.Name, tc.Arguments))
		}
		toolSummary := "Provider requested tool execution: " + strings.Join(parts, "; ")
		if clean != "" {
			text = clean + "\n\n" + toolSummary
		} else {
			text = toolSummary
		}
	} else if clean != "" {
		text = clean
	} else if text != "" {
		// If clean was stripped leaving empty text (e.g. only thought tags returned),
		// extract inner thoughts so the caller gets an actual answer instead of an error!
		extracted := tools.ExtractThoughts(text)
		if strings.TrimSpace(extracted) != "" {
			text = strings.TrimSpace(extracted)
		}
	}
	if text == "" && thinking.Len() > 0 {
		text = strings.TrimSpace(thinking.String())
	}
	if text == "" {
		return nil, errors.New("the provider returned an empty answer")
	}
	return &AskResult{Provider: served, Text: text, ElapsedMs: time.Since(start).Milliseconds()}, nil
}

// Status summarises the gateway and the pool for an MCP client.
func (b *PoolBackend) Status(context.Context) (map[string]any, error) {
	base := proxy.ProxyBase()
	up := proxy.ProxyUp()
	res := map[string]any{
		"gateway": map[string]any{
			"running": up,
			"baseUrl": base,
			"clients": map[string]string{
				"anthropic (Claude Code)":          "ANTHROPIC_BASE_URL=" + base,
				"openai (Codex, Cursor, opencode)": "OPENAI_BASE_URL=" + base + "/v1",
				"gemini (Gemini CLI, Antigravity)": "GOOGLE_GEMINI_BASE_URL=" + base,
			},
			"hint": "start with `amux start`; run IDEs in sandbox with `amux run <ide>` or wire tools with `amux mcp install`",
		},
	}
	if ps, err := b.Providers(); err == nil {
		usable := 0
		byTier := map[string]int{}
		for _, p := range ps {
			if p.InPool && p.Configured {
				usable++
				byTier[p.Tier]++
			}
		}
		res["providers"] = map[string]any{"total": len(ps), "usable": usable, "byTier": byTier}
	} else {
		res["providers"] = map[string]any{"error": err.Error()}
	}
	return res, nil
}
