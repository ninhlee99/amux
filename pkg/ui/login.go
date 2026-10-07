package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"amux-accounts/pkg/auth/oauth"
	"amux-accounts/pkg/browser"
	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// loginFlags holds optional non-interactive credentials passed on the CLI.
type loginFlags struct {
	model      string
	token      string // access token / API key / sessionKey
	baseURL    string // endpoint / base URL for custom API provider
	name       string // friendly name or identifier
	cookie     string // raw Cookie header or name=value
	refresh    string // refresh token when the web session exposes one
	useBrowser bool   // open Chromium via CDP and capture cookie (default when no token/cookie)
	noBrowser  bool
	defBrowser bool // open the default browser (like OAuth) and paste the cookie back
	isOAuth    bool // trigger standalone OAuth flow
	isDevice   bool // trigger device code flow
	isManual   bool // trigger manual code entry flow
}

func parseLoginFlags(args []string) (providerName string, f loginFlags, rest []string) {
	if len(args) == 0 {
		return "", f, nil
	}
	providerName = strings.ToLower(args[0])
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--model=") {
			f.model = strings.TrimPrefix(arg, "--model=")
			continue
		}
		if strings.HasPrefix(arg, "--api-key=") || strings.HasPrefix(arg, "--token=") || strings.HasPrefix(arg, "--access-token=") || strings.HasPrefix(arg, "--key=") {
			idx := strings.Index(arg, "=")
			f.token = arg[idx+1:]
			continue
		}
		if strings.HasPrefix(arg, "--endpoint=") || strings.HasPrefix(arg, "--base-url=") || strings.HasPrefix(arg, "--url=") {
			idx := strings.Index(arg, "=")
			f.baseURL = arg[idx+1:]
			continue
		}
		if strings.HasPrefix(arg, "--name=") || strings.HasPrefix(arg, "--provider=") {
			idx := strings.Index(arg, "=")
			f.name = arg[idx+1:]
			continue
		}
		if strings.HasPrefix(arg, "--cookie=") {
			f.cookie = strings.TrimPrefix(arg, "--cookie=")
			continue
		}
		if strings.HasPrefix(arg, "--refresh=") || strings.HasPrefix(arg, "--refresh-token=") {
			idx := strings.Index(arg, "=")
			f.refresh = arg[idx+1:]
			continue
		}

		switch arg {
		case "--model":
			if i+1 < len(args) {
				f.model = args[i+1]
				i++
			}
		case "--token", "--access-token", "--api-key", "--key":
			if i+1 < len(args) {
				f.token = args[i+1]
				i++
			}
		case "--endpoint", "--base-url", "--url":
			if i+1 < len(args) {
				f.baseURL = args[i+1]
				i++
			}
		case "--name", "--provider":
			if i+1 < len(args) {
				f.name = args[i+1]
				i++
			}
		case "--cookie":
			if i+1 < len(args) {
				f.cookie = args[i+1]
				i++
			}
		case "--refresh", "--refresh-token":
			if i+1 < len(args) {
				f.refresh = args[i+1]
				i++
			}
		case "--browser":
			f.useBrowser = true
		case "--no-browser":
			f.noBrowser = true
		case "--default-browser", "--open":
			f.defBrowser = true
		case "--oauth":
			f.isOAuth = true
		case "--device", "-d":
			f.isDevice = true
		case "--manual", "-m":
			f.isManual = true
		default:
			rest = append(rest, arg)
		}
	}
	return providerName, f, rest
}

// CmdLogin handles login. Default for chatgpt/claude/gemini-web: open the login
// page in the user's own default browser (where they are usually already signed
// in) and take the cookie as a paste. Pass --browser for a dedicated window with
// automatic CDP capture, --token/--cookie to skip the browser entirely, or
// --no-browser to paste without opening anything. Pass --oauth or login
// codex/antigravity for standalone OAuth.
func CmdLogin(args []string) {
	var target string
	var flags loginFlags
	if len(args) == 0 {
		fmt.Println("Select provider to login:")
		fmt.Println("  -- Subscriptions (IDE Plans) --")
		fmt.Println("  [1] claude       Claude Code OAuth / Web")
		fmt.Println("  [2] codex        OpenAI Codex OAuth")
		fmt.Println("  [3] agy          Google Antigravity OAuth")
		fmt.Println("  [4] cursor       Cursor Token / API Key")
		fmt.Println("  -- Free Web Accounts (AI Gateway Rotation) --")
		fmt.Println("  [5] chatgpt      ChatGPT Web (Browser / Session)")
		fmt.Println("  [6] gemini-web   Gemini Web (Browser / Cookies)")
		fmt.Println("  [7] claude-web   Claude Web (Session Key)")
		fmt.Println("  -- Metered API Keys --")
		fmt.Println("  [8] openai       OpenAI API Key")
		fmt.Println("  [9] gemini       Google AI Studio API Key")
		fmt.Println("  [10] groq        Groq API Key")
		fmt.Println("  [11] kimi        Moonshot / Kimi API Key")
		fmt.Println("  [12] grok        xAI / Grok API Key")
		ans := strings.TrimSpace(term.ReadLine("Select [1-12] (or provider name): "))
		switch strings.ToLower(ans) {
		case "1", "claude":
			target = "claude"
		case "2", "codex":
			target = "codex"
		case "3", "agy", "antigravity":
			target = "agy"
		case "4", "cursor":
			target = "cursor"
		case "5", "chatgpt", "chatgpt-web":
			target = "chatgpt"
		case "6", "gemini-web":
			target = "gemini-web"
		case "7", "claude-web":
			target = "claude-web"
		case "8", "openai":
			target = "openai"
		case "9", "gemini":
			target = "gemini"
		case "10", "groq":
			target = "groq"
		case "11", "kimi":
			target = "kimi"
		case "12", "grok":
			target = "grok"
		default:
			if ans != "" {
				target = strings.ToLower(ans)
			} else {
				fmt.Println("Invalid selection. Supported providers: claude, codex, agy, cursor, chatgpt, gemini-web, claude-web, openai, gemini, groq, kimi, grok")
				return
			}
		}
	} else {
		target, flags, _ = parseLoginFlags(args)
	}
	switch target {
	case "codex", "codex-cli":
		opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
		if err := oauth.InteractiveOAuthWithOptions("codex", opts); err != nil {
			fmt.Printf("Login failed: %v\n", err)
			return
		}
		CmdAccounts()
	case "agy", "antigravity":
		opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
		if err := oauth.InteractiveOAuthWithOptions("antigravity", opts); err != nil {
			fmt.Printf("Login failed: %v\n", err)
			return
		}
		CmdAccounts()
	case "claude-code", "claude-oauth":
		opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
		if err := oauth.InteractiveOAuthWithOptions("claude", opts); err != nil {
			fmt.Printf("Login failed: %v\n", err)
			return
		}
		CmdAccounts()
	case "chatgpt", "chatgpt-web", "chatgptweb":
		loginChatGPT(flags)
	case "claude", "claude-web", "claudeweb":
		if target == "claude" && !flags.isOAuth && !flags.isDevice && !flags.isManual && flags.token == "" && flags.cookie == "" {
			fmt.Println("Choose login method for Claude:")
			fmt.Println("  [1] Claude Code OAuth (Auto-login via browser -> Access Token & Refresh Token) [Default]")
			fmt.Println("  [2] Claude Web (sessionKey cookie for claude.ai web session pool)")
			ans := term.ReadLine("Select [1/2] (Enter = 1): ")
			if ans == "" || ans == "1" {
				flags.isOAuth = true
			}
		}
		if flags.isOAuth || flags.isDevice || flags.isManual {
			opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
			if err := oauth.InteractiveOAuthWithOptions("claude", opts); err != nil {
				fmt.Printf("Login failed: %v\n", err)
				return
			}
			CmdAccounts()
		} else {
			loginClaude(flags)
		}
	case "gemini-web", "geminiweb":
		loginGeminiWeb(flags)
	case "gemini", "google-ai-studio", "geminiapi":
		loginGemini(flags)
	case "github", "github-models":
		loginGitHubModels(flags)
	case "groq":
		loginGroq(flags)
	case "kimi", "moonshot", "kimiapi":
		if flags.isOAuth || flags.isDevice {
			if err := oauth.InteractiveOAuth("kimi", ""); err != nil {
				fmt.Printf("Login failed: %v\n", err)
				return
			}
			CmdAccounts()
		} else {
			loginKimi(flags)
		}
	case "grok", "xai", "grokapi":
		if flags.isOAuth || flags.isDevice {
			if err := oauth.InteractiveOAuth("grok", ""); err != nil {
				fmt.Printf("Login failed: %v\n", err)
				return
			}
			CmdAccounts()
		} else {
			loginGrok(flags)
		}
	case "cursor":
		loginCursor(flags)
	case "openai", "openai-api":
		loginOpenAI(flags)
	default:
		fmt.Printf("Unknown provider %q. Supported: claude, codex, agy, gemini, gemini-web, chatgpt, claude-web, cursor, openai, groq, kimi, grok (or run: amux login)\n", target)
	}
}

func loginCursor(f loginFlags) {
	fmt.Println("== Login: Cursor ==")
	key := strings.TrimSpace(f.token)
	if key == "" {
		key = strings.TrimSpace(term.ReadLine("Enter OpenAI API Key / Token for Cursor: "))
	}
	if key == "" {
		fmt.Println("API key required.")
		return
	}
	model := f.model
	if model == "" {
		model = "gpt-6.1-sol"
	}
	path := provider.DefaultAccountsPath()
	id, priorityFloor, multi := nextPoolID("cursor:api")
	priority := 1
	if multi {
		priority = priorityFloor
	}
	cfg := provider.ProviderConfig{
		ID:       id,
		Type:     "openai_compatible",
		IDE:      "cursor",
		Priority: priority,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   key,
		Model:    model,
		Plan:     "pro",
	}
	if err := provider.AddOrUpdateProvider(path, cfg); err != nil {
		fmt.Printf("Error saving: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("Saved Cursor account as %s.\n", id)
	fmt.Printf("Tip: To include %s in auto-failover pool, run: amux pool add %s\n", id, id)
	CmdAccounts()
}

// openForManualPaste opens target in the user's default browser (the same way
// the OAuth flows do) so they can sign in in their normal window. The cookie
// cannot be read from there, so the caller prompts for a paste afterwards.
func openForManualPaste(target browser.WebLoginTarget) {
	fmt.Printf("Opening %s in your default browser…\n", target.StartURL)
	if err := browser.OpenDefaultBrowser(target.StartURL); err != nil {
		fmt.Printf("Could not open a browser automatically (%v).\n", err)
		fmt.Printf("Open this URL manually:\n  %s\n", target.StartURL)
	}
	fmt.Println(browser.ManualLoginHint(target))
}

func readLinePrompt(prompt string) string {
	return term.ReadLine(prompt)
}

func nextPoolID(prefix string) (id string, priorityFloor int, hasExisting bool) {
	f, _ := provider.LoadConfigFile(provider.DefaultAccountsPath())
	n := 0
	maxPriority := 0
	if f != nil {
		for _, p := range f.Providers {
			if pre, num, ok := types.ParseID(p.ID); ok && pre == prefix {
				if num > n {
					n = num
				}
				hasExisting = true
				if p.Priority > maxPriority {
					maxPriority = p.Priority
				}
			}
		}
	}
	return types.FormatID(prefix, n+1), maxPriority + 1, hasExisting
}

func loginChatGPT(f loginFlags) {
	fmt.Println("== Login: ChatGPT Web ==")

	sessionCookie := ""
	access := strings.TrimSpace(f.token)
	refresh := strings.TrimSpace(f.refresh)

	if f.cookie != "" {
		sessionCookie = browser.ParseCookieHeader(f.cookie, "__Secure-next-auth.session-token")
		if sessionCookie == "" {
			sessionCookie = strings.TrimSpace(f.cookie)
		}
	}

	// CDP capture is opt-in via --browser. By default we open the user's own
	// browser, where they are usually already signed in, and take a paste.
	wantBrowser := f.useBrowser && access == "" && sessionCookie == ""
	useDefaultBrowser := !wantBrowser && !f.noBrowser && access == "" && sessionCookie == ""
	if wantBrowser {
		fmt.Println("Opening dedicated browser (CDP capture, no Keychain)…")
		tok, err := browser.CaptureCookieViaBrowser(browser.ChatGPTWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to your default browser…")
			f.defBrowser = true
		} else {
			sessionCookie = tok
			fmt.Println("Captured session cookie from browser.")
		}
	}
	if sessionCookie == "" && access == "" && !wantBrowser {
		if tok, bName, err := browser.ExtractCookie("chatgpt.com", "__Secure-next-auth.session-token"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted ChatGPT session token from %s!\n", bName)
			sessionCookie = tok
		}
	}

	if sessionCookie == "" && access == "" {
		if useDefaultBrowser || f.defBrowser {
			openForManualPaste(browser.ChatGPTWebLogin)
		}
		fmt.Println("Paste from chatgpt.com DevTools, or leave blank to cancel:")
		raw := readLinePrompt("  session-token cookie OR accessToken: ")
		if raw == "" {
			fmt.Println("Cancelled.")
			return
		}
		if strings.HasPrefix(raw, "eyJ") || strings.HasPrefix(raw, "sk-") {
			access = raw
		} else {
			sessionCookie = browser.ParseCookieHeader(raw, "__Secure-next-auth.session-token")
			if sessionCookie == "" {
				sessionCookie = raw
			}
		}
	}

	accountEmail := ""
	if access == "" && sessionCookie != "" {
		sess, err := browser.FetchChatGPTSession(sessionCookie)
		if err != nil {
			fmt.Printf("Web session exchange failed: %v\n", err)
			fmt.Println("Falling back to storing the raw cookie/token as-is.")
			access = sessionCookie
		} else {
			access = sess.AccessToken
			if refresh == "" {
				refresh = sess.RefreshToken
			}
			accountEmail = sess.Email
			if sess.Email != "" {
				fmt.Printf("Session OK for %s (expires %s).\n", sess.Email, sess.Expires)
			} else {
				fmt.Println("Session OK — access token from chatgpt.com/api/auth/session.")
			}
		}
	}
	// The refresh token is only worth asking for when the user pasted a bare
	// access token; a session cookie already refreshes itself.
	if refresh == "" && accountEmail == "" && f.token == "" && f.cookie == "" && !wantBrowser {
		if r := readLinePrompt("  refresh token (optional, Enter to skip): "); r != "" {
			refresh = r
		}
	}

	if access == "" {
		fmt.Println("No token provided. Cancelled.")
		return
	}

	if accountEmail != "" {
		backfillChatGPTAccounts()
	}
	savePoolLogin("chatgpt_web", accountEmail, func(slot provider.PoolSlot) provider.ProviderConfig {
		return provider.ProviderConfig{
			ID:           slot.ID,
			Type:         "chatgpt_web",
			Priority:     slot.Priority,
			Enabled:      slot.Enabled,
			Account:      accountEmail,
			SessionToken: access,
			RefreshToken: refresh,
			Model:        coalesceModel(f.model, "auto"),
		}
	})
}

func loginClaude(f loginFlags) {
	fmt.Println("== Login: Claude Web ==")

	key := strings.TrimSpace(f.token)
	if f.cookie != "" {
		key = browser.ParseCookieHeader(f.cookie, "sessionKey")
		if key == "" {
			key = strings.TrimSpace(f.cookie)
		}
	}

	wantBrowser := f.useBrowser && key == ""
	useDefaultBrowser := !wantBrowser && !f.noBrowser && key == ""
	cookieHeader := ""
	if wantBrowser && key == "" {
		fmt.Println("Opening dedicated browser (CDP capture, no Keychain)…")
		auth, err := browser.CaptureWebAuthViaBrowser(browser.ClaudeWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to your default browser…")
			f.defBrowser = true
		} else {
			key = auth.SessionValue
			cookieHeader = auth.CookieHeader
			fmt.Println("Captured sessionKey from browser.")
			if cookieHeader != "" {
				n := strings.Count(cookieHeader, "=")
				fmt.Printf("Captured full cookie jar (%d cookies).\n", n)
			} else {
				fmt.Println("Warning: cookie jar empty — Cloudflare cookies missing; re-run login if chat 403s.")
			}
		}
	}
	if key == "" && !wantBrowser {
		if tok, bName, err := browser.ExtractCookie("claude.ai", "sessionKey"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted claude.ai sessionKey from %s!\n", bName)
			key = tok
		}
	}

	if key == "" {
		if useDefaultBrowser || f.defBrowser {
			openForManualPaste(browser.ClaudeWebLogin)
		}
		key = readLinePrompt("Paste claude.ai sessionKey cookie (DevTools → Cookies): ")
	}
	if key == "" {
		fmt.Println("Cancelled.")
		return
	}
	if parsed := browser.ParseCookieHeader(key, "sessionKey"); parsed != "" {
		key = parsed
	}
	if cookieHeader == "" && f.cookie != "" && strings.Contains(f.cookie, "=") {
		cookieHeader = f.cookie
	}

	accountEmail := ""
	accountPlan := "free"
	if acct, err := browser.FetchClaudeAccount(key, cookieHeader); err != nil {
		fmt.Printf("Could not detect account email: %v\n", err)
		fmt.Println("Continuing without identity — re-login may create a new pool entry.")
	} else {
		accountEmail = acct.Email
		if acct.Plan != "" {
			accountPlan = acct.Plan
		}
		if accountPlan == "pro" {
			fmt.Printf("✨ Signed in as %s [Subscription: Claude Pro/Team].\n", accountEmail)
			fmt.Println("👉 Tip: This account has a paid subscription and can also be used directly with Claude Code CLI ('am add claude').")
		} else {
			fmt.Printf("ℹ️ Signed in as %s [Tier: Free] -> Configured for Claude Web proxy pool.\n", accountEmail)
		}
	}

	model := f.model
	if model == "" {
		if detected := provider.DetectClaudeWebModel(key, cookieHeader); detected != "" {
			model = detected
			fmt.Printf("Detected model: %s\n", model)
		} else {
			model = tools.DefaultClaudeSonnetModel
		}
	}

	savePoolLogin("claude_web", accountEmail, func(slot provider.PoolSlot) provider.ProviderConfig {
		return provider.ProviderConfig{
			ID:         slot.ID,
			Type:       "claude_web",
			Priority:   slot.Priority,
			Enabled:    slot.Enabled,
			Account:    accountEmail,
			Plan:       accountPlan,
			SessionKey: key,
			Cookies:    cookieHeader,
			Model:      model,
		}
	})
}

// backfillChatGPTAccounts learns the email of chatgpt_web rows saved without
// one (an earlier login whose session exchange failed), so logging in to the
// same account again updates that row instead of adding a second one.
func backfillChatGPTAccounts() {
	path := provider.DefaultAccountsPath()
	f, err := provider.LoadConfigFile(path)
	if err != nil || f == nil {
		return
	}
	for _, p := range f.Providers {
		if p.Type != "chatgpt_web" || strings.TrimSpace(p.Account) != "" || p.SessionToken == "" {
			continue
		}
		if email := chatgptTokenEmail(p.SessionToken); email != "" {
			_ = provider.SetProviderAccount(path, p.ID, email)
		}
	}
}

// backfillGeminiAccounts learns the email of gemini_web rows saved without
// one (an earlier login whose account was not yet captured), so logging in to
// the same account again updates that row instead of adding a second one.
func backfillGeminiAccounts() {
	path := provider.DefaultAccountsPath()
	f, err := provider.LoadConfigFile(path)
	if err != nil || f == nil {
		return
	}
	for _, p := range f.Providers {
		if p.Type != "gemini_web" || strings.TrimSpace(p.Account) != "" {
			continue
		}
		cookie := p.Cookies
		if cookie == "" && p.SessionKey != "" {
			cookie = "__Secure-1PSID=" + p.SessionKey
		}
		if acct, err := browser.FetchGeminiAccount(p.SessionKey, cookie); err == nil && acct.Email != "" {
			_ = provider.SetProviderAccount(path, p.ID, acct.Email)
		}
	}
}

// chatgptTokenEmail returns the account email behind a stored ChatGPT
// credential: an access-token JWT is read offline, a session cookie is
// exchanged at chatgpt.com. "" when unknown (expired, offline).
func chatgptTokenEmail(tok string) string {
	if email, _, _ := oauth.ParseCodexClaims(tok); strings.Contains(email, "@") {
		return email
	}
	if sess, err := browser.FetchChatGPTSession(tok); err == nil {
		return sess.Email
	}
	return ""
}

func coalesceModel(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// savePoolLogin resolves the pool slot (same account → relogin) and persists
// credentials under a stable identity ID when email is known.
func savePoolLogin(providerType, accountEmail string, build func(provider.PoolSlot) provider.ProviderConfig) {
	path := provider.DefaultAccountsPath()
	slot := provider.ResolvePoolSlot(path, providerType, accountEmail)
	cfg := build(slot)
	if err := provider.UpsertPoolProvider(path, cfg, slot.RenameFrom); err != nil {
		fmt.Printf("Error saving: %v\n", err)
		return
	}
	proxy.Sync()
	label := identity.ProductOfType(providerType)
	ref := slot.ID
	if accountEmail != "" {
		label += " (" + accountEmail + ")"
		ref = accountEmail
	}
	if slot.Relogin {
		fmt.Printf("✓ Re-logged in %s — existing entry updated.\n", label)
	} else {
		fmt.Printf("✓ Saved %s.\n", label)
	}
	if slot.Enabled != nil && !*slot.Enabled {
		fmt.Printf("Tip: it is not in the auto-failover pool; add it with: amux pool add %s\n", ref)
	}
	CmdAccounts()
}

func loginGemini(f loginFlags) {
	fmt.Println("== Login: Gemini (AI Studio API key) ==")

	key := strings.TrimSpace(f.token)
	if key == "" && !f.noBrowser {
		// Gemini needs an API key; open AI Studio so user can copy one.
		// (Cookie alone cannot call generativelanguage.googleapis.com.)
		fmt.Println("Opening AI Studio — create/copy an API key, then paste it below.")
		_ = exec.Command("open", "https://aistudio.google.com/apikey").Start()
	}
	if key == "" {
		key = readLinePrompt("Paste Google AI Studio API key (Enter = $GOOGLE_AI_STUDIO_KEY): ")
	}
	if key == "" {
		key = "env:GOOGLE_AI_STUDIO_KEY"
	}

	id, priorityFloor, multi := nextPoolID(provider.PoolIDPrefix("gemini"))
	priority := provider.PriorityAPIGemini
	if multi {
		priority = priorityFloor
	}
	model := f.model
	if model == "" {
		if detected := provider.DetectGeminiModel(key); detected != "" {
			model = detected
			fmt.Printf("Detected model: %s\n", model)
		} else {
			model = "gemini-3.8-flash"
		}
	}
	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:       id,
		Type:     "gemini",
		Priority: priority,
		APIKey:   key,
		Model:    model,
		Cookies:  f.cookie,
	})
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	proxy.Sync()
	fmt.Printf("Saved Gemini as %s.\n", id)
	fmt.Printf("Tip: To include %s in auto-failover pool, run: amux pool add %s\n", id, id)
	CmdAccounts()
}

func loginGeminiWeb(f loginFlags) {
	fmt.Println("== Login: Gemini Web (gemini.google.com) ==")

	cookieHeader := strings.TrimSpace(f.cookie)
	key := strings.TrimSpace(f.token)
	wantBrowser := f.useBrowser && cookieHeader == "" && key == ""
	useDefaultBrowser := !wantBrowser && !f.noBrowser && cookieHeader == "" && key == ""
	if wantBrowser {
		fmt.Println("Opening dedicated browser (CDP capture) — sign in to gemini.google.com…")
		auth, err := browser.CaptureWebAuthViaBrowser(browser.GeminiWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to your default browser…")
			f.defBrowser = true
		} else {
			key = auth.SessionValue
			cookieHeader = auth.CookieHeader
			fmt.Println("Captured __Secure-1PSID from browser.")
			if cookieHeader != "" {
				fmt.Printf("Captured cookie jar (%d cookies).\n", strings.Count(cookieHeader, "="))
			}
		}
	}
	if cookieHeader == "" && key == "" && !wantBrowser {
		if tok, bName, err := browser.ExtractCookie("google.com", "__Secure-1PSID"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted Google __Secure-1PSID from %s!\n", bName)
			key = tok
			cookieHeader = "__Secure-1PSID=" + tok
			if ts, _, _ := browser.ExtractCookie("google.com", "__Secure-1PSIDTS"); ts != "" {
				cookieHeader += "; __Secure-1PSIDTS=" + ts
			}
		}
	}
	if cookieHeader == "" && key == "" {
		if useDefaultBrowser || f.defBrowser {
			openForManualPaste(browser.GeminiWebLogin)
		}
		cookieHeader = readLinePrompt("Paste the __Secure-1PSID value, or a full Cookie header containing it: ")
	}
	if key == "" && cookieHeader != "" {
		key = browser.ParseCookieHeader(cookieHeader, "__Secure-1PSID")
	}
	if cookieHeader == "" && key != "" {
		cookieHeader = "__Secure-1PSID=" + key
	}
	if cookieHeader == "" {
		fmt.Println("Cancelled.")
		return
	}

	accountEmail := ""
	accountPlan := "free"
	if acct, err := browser.FetchGeminiAccount(key, cookieHeader); err != nil {
		fmt.Printf("Could not detect account email: %v\n", err)
		fmt.Println("Continuing without identity — re-login may create a new pool entry.")
	} else {
		accountEmail = acct.Email
		if acct.Plan != "" {
			accountPlan = acct.Plan
		}
		if accountPlan == "pro" {
			fmt.Printf("✨ Signed in as %s [Subscription: Gemini Advanced / Google One].\n", accountEmail)
		} else {
			fmt.Printf("ℹ️ Signed in as %s [Tier: Free] -> Configured for Gemini Web proxy pool.\n", accountEmail)
		}
	}

	model := f.model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	if accountEmail != "" {
		backfillGeminiAccounts()
	}
	savePoolLogin("gemini_web", accountEmail, func(slot provider.PoolSlot) provider.ProviderConfig {
		return provider.ProviderConfig{
			ID:         slot.ID,
			Type:       "gemini_web",
			Priority:   slot.Priority,
			Enabled:    slot.Enabled,
			Account:    accountEmail,
			Plan:       accountPlan,
			SessionKey: key,
			Cookies:    cookieHeader,
			Model:      model,
		}
	})
}

// githubModelsRetired: GitHub retired GitHub Models (playground, catalog and
// inference API) on 2026-07-30 — models.github.ai no longer serves requests.
const githubModelsRetired = true

func loginGitHubModels(f loginFlags) {
	fmt.Println("== Login: GitHub Models ==")
	if githubModelsRetired {
		fmt.Println("GitHub Models was retired by GitHub on 2026-07-30 (the inference API no longer answers).")
		fmt.Println("Use another provider instead, e.g. `amux login gemini`, `amux login groq`, `amux login kimi` or `amux login grok`.")
		return
	}
	tok := strings.TrimSpace(f.token)
	if tok == "" {
		tok = readLinePrompt("GitHub PAT (Enter = $GITHUB_MODELS_TOKEN): ")
	}
	if tok == "" {
		tok = "env:GITHUB_MODELS_TOKEN"
	}
	id, priorityFloor, multi := nextPoolID("githubapi")
	priority := provider.PriorityAPIGitHub
	if multi {
		priority = priorityFloor
	}
	model := f.model
	if model == "" {
		model = "gpt-6.1-sol"
	}
	cfg := provider.ProviderConfig{
		ID:       id,
		Type:     "openai_compatible",
		Priority: priority,
		BaseURL:  "https://models.github.ai/inference",
		APIKey:   tok,
		Model:    model,
	}
	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("Saved GitHub Models as %s.\n", id)
	CmdAccounts()
}

type openAICompatSpec struct {
	Name         string
	EnvVar       string
	DefaultURL   string
	DefaultModel string
	IDPrefix     string
	Priority     int
}

func loginOpenAICompat(spec openAICompatSpec, f loginFlags) {
	fmt.Printf("== Login: %s ==\n", spec.Name)
	key := strings.TrimSpace(f.token)
	if key == "" {
		key = readLinePrompt(fmt.Sprintf("%s API key (Enter = $%s): ", spec.Name, spec.EnvVar))
	}
	if key == "" {
		key = "env:" + spec.EnvVar
	}
	model := f.model
	if model == "" {
		model = spec.DefaultModel
	}
	id, priorityFloor, multi := nextPoolID(spec.IDPrefix)
	priority := spec.Priority
	if multi {
		priority = priorityFloor
	}
	cfg := provider.ProviderConfig{
		ID:       id,
		Type:     "openai_compatible",
		Priority: priority,
		BaseURL:  spec.DefaultURL,
		APIKey:   key,
		Model:    model,
	}
	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("Saved %s as %s (model: %s).\n", spec.Name, id, model)
	fmt.Printf("Tip: To include %s in auto-failover pool, run: amux pool add %s\n", id, id)
	CmdAccounts()
}

func loginGroq(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "Groq",
		EnvVar:       "GROQ_API_KEY",
		DefaultURL:   "https://api.groq.com/openai/v1",
		DefaultModel: "llama-3.3-70b-versatile",
		IDPrefix:     "groq:api",
		Priority:     provider.PriorityAPIGroq,
	}, f)
}

func loginKimi(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "Kimi (Moonshot AI)",
		EnvVar:       "KIMI_API_KEY",
		DefaultURL:   "https://api.moonshot.ai/v1",
		DefaultModel: "kimi-k2.7-code",
		IDPrefix:     "kimi:api",
		Priority:     provider.PriorityAPIKimi,
	}, f)
}

func loginGrok(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "Grok (xAI)",
		EnvVar:       "XAI_API_KEY",
		DefaultURL:   "https://api.x.ai/v1",
		DefaultModel: "grok-4.7",
		IDPrefix:     "grok:api",
		Priority:     provider.PriorityAPIGrok,
	}, f)
}

func loginOpenAI(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "OpenAI",
		EnvVar:       "OPENAI_API_KEY",
		DefaultURL:   "https://api.openai.com/v1",
		DefaultModel: "gpt-6.1-sol",
		IDPrefix:     "openai:api",
		Priority:     provider.PriorityAPIOpenAI,
	}, f)
}

// CmdDoctorProviders live-probes every addressable adapter (pool + out-of-pool
// web accounts) with a tiny chat turn and prints OK/FAIL.
// Free web accounts are listed but not probed by default (quota burn) —
// set AM_DOCTOR_WEB=1 to include them.
func CmdDoctorProviders() {
	adapters, err := provider.LoadAllAddressable(provider.DefaultAccountsPath())
	if err != nil {
		fmt.Printf("load accounts: %v\n", err)
		return
	}
	if len(adapters) == 0 {
		fmt.Println("No providers. Try: amux login chatgpt|claude|gemini")
		return
	}
	probeFreeWeb := os.Getenv("AM_DOCTOR_WEB") == "1"
	probeTools := os.Getenv("AM_DOCTOR_TOOLS") == "1"
	fmt.Println(term.Bold("=== amux doctor providers (live 1-turn probe) ==="))
	if !probeFreeWeb {
		fmt.Println(term.Dim("free web skipped (set AM_DOCTOR_WEB=1 to probe)"))
	}
	if probeTools {
		fmt.Println(term.Dim("AM_DOCTOR_TOOLS=1: requesting Bash tool_call when supported"))
	}
	req := &types.ChatRequest{
		Model:    "default",
		Messages: []types.ChatMessage{{Role: "user", Content: "Reply with exactly: OK"}},
	}
	if probeTools {
		req.Tools = []types.ToolDef{{
			Name:        "Bash",
			Description: "run shell",
			InputSchema: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
		}}
		req.Messages = []types.ChatMessage{{
			Role:    "user",
			Content: "Use a tool_call for Bash with command: echo doctor-ok. Do not answer in prose.",
		}}
	}
	for _, a := range adapters {
		tag := doctorWebTag(a)
		if doctorIsFreeWeb(a) && !probeFreeWeb {
			fmt.Printf("%s  %-16s%s  skipped\n", term.Dim("SKIP"), a.ID(), tag)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		ch, err := a.SendMessageStream(ctx, req)
		if err != nil {
			cancel()
			fmt.Printf("%s  %-16s%s  %v\n", term.Red("FAIL"), a.ID(), tag, err)
			continue
		}
		var got strings.Builder
		var streamErr error
		var gotTools []types.ToolCall
		for chunk := range ch {
			if chunk.Error != nil {
				streamErr = chunk.Error
				break
			}
			got.WriteString(chunk.Content)
			if len(chunk.ToolCalls) > 0 {
				gotTools = append(gotTools, chunk.ToolCalls...)
			}
		}
		cancel()
		if streamErr != nil {
			fmt.Printf("%s  %-16s%s  %v\n", term.Red("FAIL"), a.ID(), tag, streamErr)
			continue
		}
		preview := strings.ReplaceAll(strings.TrimSpace(got.String()), "\n", " ")
		if len(preview) > 60 {
			preview = preview[:60] + "…"
		}
		if probeTools {
			if len(gotTools) == 0 {
				fmt.Printf("%s  %-16s%s  no tool_calls (got %q)\n", term.Red("FAIL"), a.ID(), tag, preview)
				continue
			}
			fmt.Printf("%s    %-16s%s  tools=%s\n", term.Green("OK"), a.ID(), tag, tools.FormatToolCalls(gotTools))
			continue
		}
		if preview == "" {
			fmt.Printf("%s  %-16s%s  empty reply\n", term.Red("FAIL"), a.ID(), tag)
			continue
		}
		fmt.Printf("%s    %-16s%s  %q\n", term.Green("OK"), a.ID(), tag, preview)
	}
}

type supportsToolsProbe interface {
	SupportsTools() bool
}

type planProbe interface {
	Plan() string
}

func doctorWebTag(a types.ProviderAdapter) string {
	var parts []string
	if p, ok := a.(supportsToolsProbe); ok && !p.SupportsTools() {
		parts = append(parts, "text-only web")
	}
	if doctorIsFreeWeb(a) {
		parts = append(parts, "free")
	}
	if len(parts) == 0 {
		return ""
	}
	return term.Dim(" [" + strings.Join(parts, ", ") + "]")
}

func doctorIsFreeWeb(a types.ProviderAdapter) bool {
	p, ok := a.(planProbe)
	if !ok {
		return strings.Contains(strings.ToLower(a.ID()), "free")
	}
	plan := strings.ToLower(strings.TrimSpace(p.Plan()))
	if plan == "free" {
		return true
	}
	return strings.Contains(strings.ToLower(a.ID()), "free")
}

func loadProviderRows() []provider.ProviderConfig {
	file, err := provider.LoadConfigFile(provider.DefaultAccountsPath())
	var rows []provider.ProviderConfig
	if err == nil && file != nil {
		rows = append(rows, file.Providers...)
	}
	hasCodex := false
	for _, p := range rows {
		if p.Type == "codex_cli" {
			hasCodex = true
			break
		}
	}
	if !hasCodex {
		if row, ok := provider.CodexAutoRow(rows); ok {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Priority < rows[j].Priority })
	return rows
}

// AccountsTable prints the account table shown by `amux accounts`; the CLI
// sets it so login output matches that command.
var AccountsTable func()

// CmdAccounts lists every saved account (after a login).
func CmdAccounts() {
	if AccountsTable != nil {
		AccountsTable()
		return
	}
	CmdAccountsFilter("")
}

// CollectFlatAccounts compiles all accounts into a flat list structure without nested groups.
func CollectFlatAccounts(filter string) []types.Account {
	var accounts []types.Account
	filter = strings.ToLower(strings.TrimSpace(filter))
	for _, tool := range profile.ToolNames(profile.LoadConfig()) {
		if tool == "codex" {
			continue
		}
		for _, p := range profile.ListProfiles(tool) {
			acct := p.ToAccount()
			if filter != "" && !strings.EqualFold(acct.Provider, filter) && !strings.Contains(strings.ToLower(acct.ID), filter) {
				continue
			}
			accounts = append(accounts, acct)
		}
	}
	for _, p := range loadProviderRows() {
		acct := p.ToAccount()
		if filter != "" && !strings.EqualFold(acct.Provider, filter) && !strings.Contains(strings.ToLower(acct.ID), filter) {
			continue
		}
		accounts = append(accounts, acct)
	}
	return accounts
}

// CmdAccountsFilter lists accounts in a clean flat table.
// filter may be a tool/provider hint (claude, codex, gemini, cursor) or empty = all.
func CmdAccountsFilter(filter string) {
	subtitle := "flat accounts list · ACTIVE=Yes means in rotate"
	if filter != "" {
		subtitle = "filter " + filter + " · " + subtitle
	}
	term.Header("amux accounts", subtitle)

	accounts := CollectFlatAccounts(filter)
	if len(accounts) == 0 {
		term.Warn("No accounts. amux login [claude|codex|gemini|cursor]")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, term.Dim("ID\tPROVIDER\tTYPE\tAUTH_TYPE\tUSAGE %\tACTIVE"))
	for _, a := range accounts {
		activeStr := "No"
		if a.Active {
			activeStr = "Yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.1f%%\t%s\n", a.ID, a.Provider, a.Type, a.AuthType, a.UsagePercent, activeStr)
	}
	w.Flush()
	term.PanelEnd()
}

// CmdAPI handles 'am api add', 'am api rm', 'am api ls'.
func CmdAPI(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: amux api [add | rm | ls]")
		return
	}

	switch args[0] {
	case "ls", "list":
		CmdAccounts()
	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: amux api rm <name>")
			return
		}
		if err := provider.RemoveProvider(provider.DefaultAccountsPath(), args[1]); err != nil {
			fmt.Printf("Error removing provider: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("Removed provider %q from pool\n", args[1])
	case "add":
		name := ""
		endpoint := ""
		apiKey := ""
		model := "default"
		priority := provider.PriorityAPICustom

		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--endpoint":
				if i+1 < len(args) {
					endpoint = args[i+1]
					i++
				}
			case "--api-key":
				if i+1 < len(args) {
					apiKey = args[i+1]
					i++
				}
			case "--model":
				if i+1 < len(args) {
					model = args[i+1]
					i++
				}
			case "--priority":
				if i+1 < len(args) {
					p, _ := strconv.Atoi(args[i+1])
					if p > 0 {
						priority = p
					}
					i++
				}
			default:
				if name == "" && !strings.HasPrefix(args[i], "-") {
					name = args[i]
				}
			}
		}

		if name == "" || endpoint == "" {
			fmt.Println("Usage: amux api add <name> --endpoint <url> --api-key <key> [--model M] [--priority N]")
			return
		}

		id := name
		// Known endpoints/names get canonical brand:api:NN prefixes so multi-key works.
		nameL := strings.ToLower(strings.TrimSpace(name))
		if provider.IsOpenRouterEndpoint(endpoint) || nameL == "openrouter" || nameL == "openrouter:api" || strings.HasPrefix(nameL, "openrouter:api:") {
			if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), "openrouter:api"); err == nil {
				id = next
			}
		} else if provider.IsKimiEndpoint(endpoint) || nameL == "kimi" || nameL == "moonshot" || strings.HasPrefix(nameL, "kimi:api:") {
			if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), "kimi:api"); err == nil {
				id = next
			}
		} else if provider.IsGrokEndpoint(endpoint) || nameL == "grok" || nameL == "xai" || strings.HasPrefix(nameL, "grok:api:") {
			if next, err := provider.NextIDForPrefix(provider.DefaultAccountsPath(), "grok:api"); err == nil {
				id = next
			}
		}

		err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
			ID:       id,
			Type:     "openai_compatible",
			Priority: priority,
			BaseURL:  endpoint,
			APIKey:   apiKey,
			Model:    model,
		})
		if err != nil {
			fmt.Printf("Error adding provider: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("Added OpenAI-compatible provider %q to pool (priority %d)\n", id, priority)
	}
}
