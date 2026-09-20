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
	name       string // custom provider name
	baseURL    string // custom endpoint base URL
	model      string
	token      string // access token / API key / sessionKey
	cookie     string // raw Cookie header or name=value
	refresh    string // refresh token when the web session exposes one
	useBrowser bool   // open Chromium via CDP and capture cookie (default when no token/cookie)
	noBrowser  bool
	defBrowser bool   // open the default browser (like OAuth) and paste the cookie back
	isOAuth    bool   // trigger standalone OAuth flow
	isDevice   bool   // trigger device code flow
	isManual   bool   // trigger manual code entry flow
}

func parseLoginFlags(args []string) (providerName string, f loginFlags, rest []string) {
	if len(args) == 0 {
		return "", f, nil
	}
	providerName = strings.ToLower(args[0])
	for i := 1; i < len(args); i++ {
		arg := args[i]
		var key, val string
		hasEq := false
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			key = strings.ToLower(parts[0])
			val = parts[1]
			hasEq = true
		} else {
			key = strings.ToLower(arg)
		}

		getVal := func() string {
			if hasEq {
				return val
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}

		switch key {
		case "--name", "--id", "--provider":
			f.name = getVal()
		case "--base-url", "--baseurl", "--url", "--endpoint":
			f.baseURL = getVal()
		case "--model":
			f.model = getVal()
		case "--token", "--access-token", "--api-key", "--apikey", "--key":
			f.token = getVal()
		case "--cookie":
			f.cookie = getVal()
		case "--refresh", "--refresh-token":
			f.refresh = getVal()
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

// CmdLogin handles login.
func CmdLogin(args []string) {
	var target string
	var flags loginFlags
	if len(args) == 0 {
		fmt.Println("Select provider to login:")
		fmt.Println("  [1] claude         (Claude Code OAuth / Device / Web Session)")
		fmt.Println("  [2] chatgpt        (ChatGPT Web - chatgpt.com Session Pool)")
		fmt.Println("  [3] codex          (OpenAI Codex CLI / OAuth)")
		fmt.Println("  [4] gemini         (Gemini Web - gemini.google.com)")
		fmt.Println("  [5] agy            (Google Antigravity / AGY OAuth Subscription)")
		fmt.Println("  [6] gemini-api     (Google AI Studio Gemini API Key)")
		fmt.Println("  [7] openai         (OpenAI API Key - api.openai.com)")
		fmt.Println("  [8] anthropic      (Anthropic API Key - api.anthropic.com)")
		fmt.Println("  [9] openrouter     (OpenRouter API Key - openrouter.ai)")
		fmt.Println("  [10] api           (Custom OpenAI-compatible API: DeepSeek, Ollama, Groq, Kimi, etc.)")
		fmt.Println("  [11] cursor        (Cursor API Key / Token)")
		ans := strings.TrimSpace(term.ReadLine("Select [1-11] (claude/chatgpt/codex/gemini/agy/gemini-api/openai/anthropic/openrouter/api/cursor): "))
		switch strings.ToLower(ans) {
		case "1", "claude":
			target = "claude"
		case "2", "chatgpt":
			target = "chatgpt"
		case "3", "codex":
			target = "codex"
		case "4", "gemini", "gemini-web", "geminiweb":
			target = "gemini"
		case "5", "agy", "antigravity":
			target = "antigravity"
		case "6", "gemini-api", "google-ai-studio", "geminiapi":
			target = "gemini-api"
		case "7", "openai", "openai-api":
			target = "openai"
		case "8", "anthropic", "anthropic-api":
			target = "anthropic"
		case "9", "openrouter", "openrouter-api":
			target = "openrouter"
		case "10", "api", "other", "deepseek", "ollama":
			target = "api"
		case "11", "cursor":
			target = "cursor"
		default:
			fmt.Println("Invalid selection. Supported: claude, chatgpt, codex, gemini, agy, gemini-api, openai, anthropic, openrouter, api, cursor")
			return
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
		if !flags.isDevice && !flags.isManual && flags.token == "" {
			fmt.Println("Choose Antigravity / AGY login method:")
			fmt.Println("  [1] Snapshot active login from IDE / System [Default - Enter]")
			fmt.Println("      (Saves the account currently signed in on your Antigravity IDE / CLI)")
			fmt.Println("  [2] Google OAuth Browser Login")
			fmt.Println("      (Direct OAuth flow - requires $ANTIGRAVITY_CLIENT_SECRET)")
			ans := strings.TrimSpace(term.ReadLine("Select [1-2] (Enter = 1): "))
			if ans == "2" || ans == "oauth" {
				opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
				if err := oauth.InteractiveOAuthWithOptions("antigravity", opts); err != nil {
					fmt.Printf("Login failed: %v\n", err)
					return
				}
			} else {
				spec, ok := profile.LookupToolSpec("antigravity")
				if !ok {
					fmt.Println("Antigravity tool spec not found.")
					return
				}
				acct := profile.DetectAccount(spec)
				if acct == "" {
					fmt.Println("No active Antigravity login detected in Keychain or ~/.gemini.")
					fmt.Println("Please sign in to Antigravity in your IDE or terminal first, then run this command again.")
					return
				}
				savedName, err := profile.CmdSave("antigravity", "")
				if err != nil {
					fmt.Printf("Failed to snapshot Antigravity account: %v\n", err)
					return
				}
				fmt.Printf("✓ Successfully saved Antigravity profile %q (%s)!\n", savedName, acct)
			}
		} else {
			opts := oauth.OAuthOptions{DeviceFlow: flags.isDevice, ManualFlow: flags.isManual || flags.noBrowser}
			if err := oauth.InteractiveOAuthWithOptions("antigravity", opts); err != nil {
				fmt.Printf("Login failed: %v\n", err)
				return
			}
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
			fmt.Println("  [1] Claude Code OAuth (Tự động mở trình duyệt & lấy Access Token) [Default - Enter]")
			fmt.Println("  [2] Login with Device / Remote Code (Lấy mã xác thực qua web - không cần callback localhost)")
			fmt.Println("  [3] Claude Web (sessionKey cookie cho claude.ai web session pool)")
			ans := strings.TrimSpace(term.ReadLine("Select [1-3] (Enter = 1): "))
			switch ans {
			case "2", "device":
				flags.isDevice = true
			case "3", "web":
				// Proceed to loginClaude web session
			default:
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
	case "gemini", "gemini-web", "geminiweb":
		loginGeminiWeb(flags)
	case "gemini-api", "google-ai-studio", "geminiapi":
		if flags.name == "" {
			flags.name = "gemini"
		}
		if flags.baseURL == "" {
			flags.baseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
		}
		loginAPI("gemini", flags)
	case "openai", "openai-api":
		loginOpenAI(flags)
	case "anthropic", "anthropic-api":
		loginAnthropic(flags)
	case "openrouter", "openrouter-api":
		loginOpenRouter(flags)
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
	case "api":
		loginAPI("api", flags)
	default:
		fmt.Printf("Unknown provider %q. Supported: claude, chatgpt, codex, gemini, agy, gemini-api, openai, anthropic, openrouter, api, cursor (or run: amux login)\n", target)
	}
}

func loginAPI(target string, f loginFlags) {
	fmt.Println("== Setup OpenAI-Compatible API Provider ==")
	key := strings.TrimSpace(f.token)
	if key == "" {
		key = strings.TrimSpace(readLinePrompt("API Key (press Enter if no auth / local): "))
	}

	model := strings.TrimSpace(f.model)
	if model == "" {
		model = strings.TrimSpace(readLinePrompt("Model name (e.g. gpt-4o, deepseek-chat, gemini-2.5-flash, llama3): "))
		if model == "" {
			model = "default"
		}
	}

	endpoint := strings.TrimSpace(f.baseURL)
	if endpoint == "" {
		defURL := "https://api.openai.com/v1"
		lowerName := strings.ToLower(f.name)
		if lowerName == "" {
			lowerName = strings.ToLower(target)
		}
		if strings.Contains(lowerName, "deepseek") {
			defURL = "https://api.deepseek.com/v1"
		} else if strings.Contains(lowerName, "ollama") {
			defURL = "http://localhost:11434/v1"
		} else if strings.Contains(lowerName, "gemini") {
			defURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
		}
		endpoint = strings.TrimSpace(readLinePrompt(fmt.Sprintf("Endpoint / Base URL [default: %s]: ", defURL)))
		if endpoint == "" {
			endpoint = defURL
		}
	}

	name := strings.TrimSpace(f.name)
	if name == "" {
		if target != "api" {
			name = target
		} else {
			defName := "api"
			lowerEnd := strings.ToLower(endpoint)
			if strings.Contains(lowerEnd, "deepseek") {
				defName = "deepseek"
			} else if strings.Contains(lowerEnd, "openai") {
				defName = "openai"
			} else if strings.Contains(lowerEnd, "ollama") {
				defName = "ollama"
			} else if strings.Contains(lowerEnd, "groq") {
				defName = "groq"
			} else if strings.Contains(lowerEnd, "googleapis") || strings.Contains(lowerEnd, "gemini") {
				defName = "gemini"
			}
			name = strings.TrimSpace(readLinePrompt(fmt.Sprintf("Provider name/id (e.g. deepseek, ollama, openai, gemini) [default: %s]: ", defName)))
			if name == "" {
				name = defName
			}
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "api"
	}

	idPrefix := name
	if !strings.HasSuffix(idPrefix, ":api") && idPrefix != "api" {
		idPrefix = fmt.Sprintf("%s:api", name)
	}
	id, priorityFloor, multi := nextPoolID(idPrefix)
	priority := provider.PriorityAPICustom
	if multi {
		priority = priorityFloor
	}

	cfg := provider.ProviderConfig{
		ID:       id,
		Type:     "openai_compatible",
		Priority: priority,
		BaseURL:  endpoint,
		APIKey:   key,
		Model:    model,
	}

	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("✓ Saved API provider %q as %s (Endpoint: %s, Model: %s).\n", name, id, endpoint, model)
	CmdAccounts()
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
		model = "gpt-4o"
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

	// If no token provided directly, try auto-extract from installed browsers first
	if sessionCookie == "" && access == "" && !f.noBrowser {
		if tok, bName, err := browser.ExtractCookie("chatgpt.com", "__Secure-next-auth.session-token"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted ChatGPT session token from %s!\n", bName)
			sessionCookie = tok
			if cf, _, err := browser.ExtractCookie("chatgpt.com", "cf_clearance"); err == nil && cf != "" {
				sessionCookie = "__Secure-next-auth.session-token=" + tok + "; cf_clearance=" + cf
			}
		}
	}

	wantBrowser := !f.noBrowser && !f.defBrowser && access == "" && sessionCookie == ""
	useDefaultBrowser := f.defBrowser && access == "" && sessionCookie == ""
	if wantBrowser {
		fmt.Println("Opening dedicated browser (automatic cookie capture via CDP) — sign in to chatgpt.com…")
		tok, err := browser.CaptureCookieViaBrowser(browser.ChatGPTWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to manual entry…")
			useDefaultBrowser = true
		} else {
			sessionCookie = tok
			fmt.Println("✓ Captured session cookie automatically from browser.")
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
	if refresh == "" && sessionCookie != "" {
		refresh = sessionCookie
	} else if refresh == "" && f.token == "" && f.cookie == "" && !wantBrowser && sessionCookie == "" {
		if r := readLinePrompt("  refresh token (optional, Enter to skip): "); r != "" {
			refresh = r
		}
	}

	if access == "" {
		fmt.Println("No token provided. Cancelled.")
		return
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

	cookieHeader := ""

	// If no cookie/token provided directly, try auto-extract from installed browsers first
	if key == "" && !f.noBrowser {
		if tok, bName, err := browser.ExtractCookie("claude.ai", "sessionKey"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted claude.ai sessionKey from %s!\n", bName)
			key = tok
			if cf, _, err := browser.ExtractCookie("claude.ai", "cf_clearance"); err == nil && cf != "" {
				cookieHeader = "sessionKey=" + tok + "; cf_clearance=" + cf
			}
		}
	}

	wantBrowser := !f.noBrowser && !f.defBrowser && key == ""
	useDefaultBrowser := f.defBrowser && key == ""
	if wantBrowser && key == "" {
		fmt.Println("Opening dedicated browser (automatic cookie capture via CDP) — sign in to claude.ai…")
		auth, err := browser.CaptureWebAuthViaBrowser(browser.ClaudeWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to manual cookie entry…")
			useDefaultBrowser = true
		} else {
			key = auth.SessionValue
			cookieHeader = auth.CookieHeader
			fmt.Println("✓ Captured sessionKey automatically from browser.")
			if cookieHeader != "" {
				n := strings.Count(cookieHeader, "=")
				fmt.Printf("Captured full cookie jar (%d cookies).\n", n)
			} else {
				fmt.Println("Warning: cookie jar empty — Cloudflare cookies missing; re-run login if chat 403s.")
			}
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
			fmt.Println("👉 Tip: This account has a paid subscription and can also be used directly with Claude Code CLI ('amux id add claude').")
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
			model = "claude-sonnet-5"
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
	switch {
	case slot.Relogin && slot.RenameFrom != "" && slot.RenameFrom != slot.ID:
		fmt.Printf("Re-logged in %s (was %s).\n", slot.ID, slot.RenameFrom)
	case slot.Relogin:
		fmt.Printf("Re-logged in %s.\n", slot.ID)
	default:
		fmt.Printf("Saved as %s.\n", slot.ID)
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
	CmdAccounts()
}

func loginGeminiWeb(f loginFlags) {
	fmt.Println("== Login: Gemini Web (gemini.google.com) ==")

	cookieHeader := strings.TrimSpace(f.cookie)
	key := strings.TrimSpace(f.token)

	// If no cookie/token provided directly, try auto-extract from installed browsers first
	if cookieHeader == "" && key == "" && !f.noBrowser {
		if tok, bName, err := browser.ExtractCookie("google.com", "__Secure-1PSID"); err == nil && tok != "" {
			fmt.Printf("✓ Auto-extracted __Secure-1PSID cookie from %s!\n", bName)
			key = tok
			cookieHeader = "__Secure-1PSID=" + tok
			if ts, _, err := browser.ExtractCookie("google.com", "__Secure-1PSIDTS"); err == nil && ts != "" {
				cookieHeader += "; __Secure-1PSIDTS=" + ts
			}
		}
	}

	wantBrowser := !f.noBrowser && !f.defBrowser && cookieHeader == "" && key == ""
	useDefaultBrowser := f.defBrowser && cookieHeader == "" && key == ""
	if wantBrowser {
		fmt.Println("Opening dedicated browser (automatic cookie capture via CDP) — sign in to gemini.google.com…")
		auth, err := browser.CaptureWebAuthViaBrowser(browser.GeminiWebLogin, 5*time.Minute)
		if err != nil {
			fmt.Printf("Browser capture failed: %v\n", err)
			if f.useBrowser {
				return
			}
			fmt.Println("Falling back to manual cookie entry…")
			useDefaultBrowser = true
		} else {
			key = auth.SessionValue
			cookieHeader = auth.CookieHeader
			fmt.Println("✓ Captured __Secure-1PSID automatically from browser.")
			if cookieHeader != "" {
				fmt.Printf("Captured cookie jar (%d cookies).\n", strings.Count(cookieHeader, "="))
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

	savePoolLogin("gemini_web", "", func(slot provider.PoolSlot) provider.ProviderConfig {
		return provider.ProviderConfig{
			ID:         slot.ID,
			Type:       "gemini_web",
			Priority:   slot.Priority,
			Enabled:    slot.Enabled,
			SessionKey: key,
			Cookies:    cookieHeader,
			Model:      f.model,
		}
	})
}

func loginOpenAI(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "OpenAI API",
		EnvVar:       "OPENAI_API_KEY",
		DefaultURL:   "https://api.openai.com/v1",
		DefaultModel: "gpt-4o",
		IDPrefix:     "openai:api",
		Priority:     provider.PriorityAPICustom,
	}, f)
}

func loginAnthropic(f loginFlags) {
	fmt.Println("== Login: Anthropic API ==")
	key := strings.TrimSpace(f.token)
	if key == "" {
		key = readLinePrompt("Anthropic API key (Enter = $ANTHROPIC_API_KEY): ")
	}
	if key == "" {
		key = "env:ANTHROPIC_API_KEY"
	}
	model := f.model
	if model == "" {
		model = "claude-3-7-sonnet-20250219"
	}
	id, priorityFloor, multi := nextPoolID("anthropic:api")
	priority := provider.PriorityAPICustom
	if multi {
		priority = priorityFloor
	}
	cfg := provider.ProviderConfig{
		ID:       id,
		Type:     "claude",
		Priority: priority,
		APIKey:   key,
		Model:    model,
	}
	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("✓ Saved Anthropic API as %s (model: %s).\n", id, model)
	CmdAccounts()
}

func loginOpenRouter(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "OpenRouter",
		EnvVar:       "OPENROUTER_API_KEY",
		DefaultURL:   "https://openrouter.ai/api/v1",
		DefaultModel: "anthropic/claude-3.7-sonnet",
		IDPrefix:     "openrouter:api",
		Priority:     provider.PriorityAPICustom,
	}, f)
}

func loginGitHubModels(f loginFlags) {
	fmt.Println("== Login: GitHub Models ==")
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
		model = "gpt-5.6-terra"
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
		DefaultURL:   "https://api.moonshot.cn/v1",
		DefaultModel: "moonshot-v1-128k",
		IDPrefix:     "kimi:api",
		Priority:     provider.PriorityAPIKimi,
	}, f)
}

func loginGrok(f loginFlags) {
	loginOpenAICompat(openAICompatSpec{
		Name:         "Grok (xAI)",
		EnvVar:       "XAI_API_KEY",
		DefaultURL:   "https://api.x.ai/v1",
		DefaultModel: "grok-2-latest",
		IDPrefix:     "grok:api",
		Priority:     provider.PriorityAPIGrok,
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
		fmt.Println("No providers. Try: amux id add")
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

func providerKindLabel(typ string) string {
	switch typ {
	case "chatgpt_web":
		return "chatgpt-web"
	case "claude_web":
		return "claude-web"
	case "gemini_web":
		return "gemini-web"
	case "gemini":
		return "gemini-api"
	case "codex_cli":
		return "codex"
	case "openai_compatible":
		return "api"
	default:
		if typ == "" {
			return "api"
		}
		return typ
	}
}

func poolMark(in bool) string {
	if in {
		return "IN"
	}
	return "OUT"
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

// CmdAccounts displays the flat identity list.
func CmdAccounts() {
	CmdAccountsFilter("")
}

// CmdAccountsFilter lists identities in a clean flat table.
func CmdAccountsFilter(filter string) {
	cfg, err := identity.LoadConfig("")
	if err != nil || len(cfg.Identities) == 0 {
		if n, _ := identity.MigrateLegacyAccounts("", ""); n > 0 {
			cfg, _ = identity.LoadConfig("")
		}
	}
	if err != nil || len(cfg.Identities) == 0 {
		fmt.Println("No identities configured. Run 'amux id add [provider]' to register an identity.")
		return
	}

	filter = strings.ToLower(strings.TrimSpace(filter))
	fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n", "ID", "EMAIL", "MODEL", "THRESHOLD", "USAGE", "ACTIVE", "AUTO-SWITCH", "RESETS IN")
	fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n", "--------------------", "--------------------------", "------------------", "--------------", "--------", "--------", "------------", "------------")

	for _, id := range cfg.Identities {
		if filter != "" && !strings.Contains(strings.ToLower(id.ID), filter) && !strings.Contains(strings.ToLower(id.Email()), filter) && !strings.Contains(strings.ToLower(id.Provider), filter) {
			continue
		}
		activeStr := "NO"
		if id.Active {
			activeStr = "YES"
		}
		if !identity.IsEnabled(id) {
			activeStr = "DISABLED"
		}
		autoStr := "ON"
		if !id.CanAutoRotate() {
			autoStr = "OFF"
		}
		if !identity.IsEnabled(id) {
			autoStr = "-"
		}
		usageStr := fmt.Sprintf("%.1f%%", id.UsagePercent)
		thresh := identity.GetAccountThreshold(id, cfg.Identities, cfg.ThresholdPct)
		threshStr := fmt.Sprintf("%.1f%%", thresh)
		resetStr := id.FormatResetTime()
		fmt.Printf("%-20s %-26s %-18s %-14s %-8s %-8s %-12s %-12s\n",
			id.ID, id.Email(), id.ModelName(), threshStr, usageStr, activeStr, autoStr, resetStr)
	}
}

// CmdPool lists accounts currently in the rotate pool (POOL=IN), flat — no group sections.
func CmdPool() {
	term.Header("amux pool", "rotate set · amux id list")
	rows := collectPoolRows()
	if len(rows) == 0 {
		term.Warn("Rotate pool empty. Use: amux id add")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, term.Dim("ID\tKIND\tPRIORITY\tMODEL"))
	for _, r := range rows {
		prio := "-"
		if r.Kind != "claude" && r.Kind != "antigravity" {
			prio = fmt.Sprintf("%d", r.Priority)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, r.Kind, prio, r.Model)
	}
	w.Flush()
	term.PanelEnd()
}

type poolRow struct {
	ID       string
	Kind     string
	Model    string
	Priority int
}

func collectPoolRows() []poolRow {
	var rows []poolRow
	for _, tool := range profile.ToolNames(profile.LoadConfig()) {
		if tool == "codex" {
			continue
		}
		for _, p := range profile.ListProfiles(tool) {
			if p.Disabled {
				continue
			}
			pName := p.Name
			if pName == "" {
				pName = p.ID
			}
			rows = append(rows, poolRow{
				ID:    pName,
				Kind:  tool,
				Model: "-",
			})
		}
	}
	for _, p := range loadProviderRows() {
		if !p.InRotatePool() {
			continue
		}
		model := p.Model
		if model == "" {
			model = "-"
		}
		rows = append(rows, poolRow{
			ID:       p.ID,
			Kind:     providerKindLabel(p.Type),
			Model:    model,
			Priority: p.Priority,
		})
	}
	return rows
}

// CmdAccountsCmd handles legacy accounts commands.
func CmdAccountsCmd(args []string) {
	if len(args) == 0 {
		CmdAccounts()
		return
	}

	switch args[0] {
	case "ls", "list":
		filter := ""
		if len(args) > 1 {
			filter = args[1]
		}
		CmdAccountsFilter(filter)
	case "rm", "delete", "remove":
		if len(args) < 2 {
			fmt.Println("Usage: amux id remove <id>")
			return
		}
		target := args[1]
		profDeleted, _ := profile.DeleteProfileAnyTool(target)
		provDeleted := provider.RemoveProvider(provider.DefaultAccountsPath(), target) == nil
		idDeleted, _ := identity.Remove("", target)
		if !profDeleted && !provDeleted && !idDeleted {
			fmt.Printf("Account %q not found in profiles or providers\n", target)
			return
		}
		proxy.Sync()
		fmt.Printf("✓ Removed %q from accounts\n", target)
	case "priority":
		if len(args) < 3 {
			fmt.Println("Usage: amux id priority <id> <N>")
			return
		}
		n, err := strconv.Atoi(args[2])
		if err != nil {
			fmt.Printf("invalid priority %q: %v\n", args[2], err)
			return
		}
		if err := provider.SetPriority(provider.DefaultAccountsPath(), args[1], n); err != nil {
			fmt.Printf("Error setting priority: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("set %s priority to %d\n", args[1], n)
	case "model":
		if len(args) < 3 {
			fmt.Println("Usage: amux id model <id> <model>")
			return
		}
		if err := provider.SetModel(provider.DefaultAccountsPath(), args[1], args[2]); err != nil {
			fmt.Printf("Error setting model: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("set %s model to %s\n", args[1], args[2])
	case "off", "disable":
		if len(args) < 2 {
			fmt.Println("Usage: amux id disable <id>")
			return
		}
		if err := provider.SetEnabled(provider.DefaultAccountsPath(), args[1], false); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("off %s — out of rotate (amux id enable %s)\n", args[1], args[1])
	case "on", "enable":
		if len(args) < 2 {
			fmt.Println("Usage: amux id enable <id>")
			return
		}
		if err := provider.SetEnabled(provider.DefaultAccountsPath(), args[1], true); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("on %s — back in rotate\n", args[1])
	default:
		fmt.Println("Usage: amux id list | amux id remove <id> | amux id select <id>")
	}
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
