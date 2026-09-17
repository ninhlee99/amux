package ui

import (
	"bufio"
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
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// loginFlags holds optional non-interactive credentials passed on the CLI.
type loginFlags struct {
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
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				f.model = args[i+1]
				i++
			}
		case "--token", "--access-token", "--api-key":
			if i+1 < len(args) {
				f.token = args[i+1]
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
			rest = append(rest, args[i])
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
	if len(args) == 0 {
		fmt.Println("Usage: amux login <provider> [--browser] [--token T] [--cookie C] [--refresh R] [--model M] [--oauth] [--device]")
		fmt.Println("Providers: chatgpt, claude, gemini, gemini-web, github, groq, kimi, grok, codex, antigravity")
		fmt.Println()
		fmt.Println("  chatgpt / claude / gemini-web  default = open your browser, paste the cookie")
		fmt.Println("  gemini (API)                   AI Studio API key")
		fmt.Println("  kimi                           Moonshot Kimi API key (or pass --oauth/--device for device flow)")
		fmt.Println("  grok                           xAI Grok API key (or pass --oauth/--device for device flow)")
		fmt.Println("  codex                          OpenAI Codex standalone OAuth (or pass --device)")
		fmt.Println("  antigravity / agy              Google Antigravity standalone OAuth (no CLI required)")
		fmt.Println("  --oauth                        trigger standalone OAuth flow directly")
		fmt.Println("  --device, -d                   trigger device code authentication flow")
		fmt.Println("  --manual, -m                   trigger manual code pasting flow")
		fmt.Println("  --token/--cookie               skip browser, use pasted credentials")
		fmt.Println("  --no-browser                   paste only, do not open any browser")
		fmt.Println("  --browser                      dedicated window + automatic CDP cookie capture")
		fmt.Println("  --default-browser, --open      explicit form of the default behaviour")
		return
	}

	target, flags, _ := parseLoginFlags(args)
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
	default:
		fmt.Printf("Unknown provider %q. Supported: chatgpt, claude, gemini, gemini-web, github, groq, kimi, grok, codex, antigravity (or run: am oauth <provider>)\n", target)
	}
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
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
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
	if refresh == "" && f.token == "" && f.cookie == "" && !wantBrowser {
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

	id, priorityFloor, multi := nextPoolID(provider.PoolIDPrefix("gemini_web"))
	priority := provider.PriorityWebGemini
	if multi {
		priority = priorityFloor
	}
	err := provider.AddOrUpdateProvider(provider.DefaultAccountsPath(), provider.ProviderConfig{
		ID:         id,
		Type:       "gemini_web",
		Priority:   priority,
		SessionKey: key,
		Cookies:    cookieHeader,
		Model:      f.model,
	})
	if err != nil {
		fmt.Printf("Error saving: %v\n", err)
		return
	}
	proxy.Sync()
	fmt.Printf("Saved Gemini Web as %s.\n", id)
	CmdAccounts()
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
		fmt.Println("No providers. Try: am login chatgpt|claude|gemini")
		return
	}
	probeFreeWeb := os.Getenv("AM_DOCTOR_WEB") == "1"
	probeTools := os.Getenv("AM_DOCTOR_TOOLS") == "1"
	fmt.Println(term.Bold("=== am doctor providers (live 1-turn probe) ==="))
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

// CmdAccounts lists every saved account: Claude profiles + web/API providers.
func CmdAccounts() {
	CmdAccountsFilter("")
}

// CmdAccountsFilter lists accounts grouped by rotate priority.
// filter may be a tool/group hint (claude, codex, agy, api, web) or empty = all.
func CmdAccountsFilter(filter string) {
	subtitle := "grouped by rotate priority · POOL=IN means rotate"
	if filter != "" {
		subtitle = "filter " + filter + " · " + subtitle
	}
	term.Header("amux accounts", subtitle)

	rows := collectAccountRows(filter)
	if len(rows) == 0 {
		term.Warn("No accounts. am add / am login / am api add")
		return
	}

	forEachAccountGroup(rows, func(title, _ string, members []accountRow) {
		printDisplaySection(title)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, term.Dim("ID\tKIND\tACCOUNT\tPLAN\tPOOL\tMODEL"))
		for _, r := range members {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.Account, r.Plan, r.Pool, r.Model)
		}
		w.Flush()
		term.PanelEnd()
	})
}

func collectAccountRows(filter string) []accountRow {
	var rows []accountRow
	for _, tool := range profile.ToolNames(profile.LoadConfig()) {
		if tool == "codex" {
			continue // Codex is represented below in loadProviderRows()
		}
		for _, p := range profile.ListProfiles(tool) {
			grp := router.ResolveProfileGroup(tool, p.Plan)
			acct := p.Account
			if acct == "" {
				acct = p.Name
			}
			pName := p.Name
			if pName == "" {
				pName = p.ID
			}
			plan := strings.ToUpper(p.Plan)
			if plan == "" {
				if tool == "claude" || tool == "antigravity" {
					plan = "PRO"
				} else {
					plan = "FREE"
				}
			}
			row := accountRow{
				Group:   grp,
				ID:      pName,
				Kind:    tool,
				Account: acct,
				Plan:    plan,
				Pool:    poolMark(!p.Disabled),
				Model:   "-",
			}
			if !matchesAccountRowFilter(filter, row) {
				continue
			}
			rows = append(rows, row)
		}
	}
	for _, p := range loadProviderRows() {
		grp := router.ResolveAccountGroup(p.ID, p.Type, p.Plan, p.Group)
		kind := providerKindLabel(p.Type)
		acct := p.Account
		if acct == "" {
			acct = "-"
		}
		model := p.Model
		if model == "" {
			model = "-"
		}
		plan := strings.ToUpper(p.Plan)
		if plan == "" {
			if p.Type == "chatgpt_web" || p.Type == "claude_web" || p.Type == "gemini_web" || p.Type == "gemini" || p.Type == "openai_compatible" {
				plan = "FREE"
			} else {
				plan = "-"
			}
		}
		row := withAPIProvider(accountRow{
			Group:    grp,
			ID:       p.ID,
			Kind:     kind,
			Account:  acct,
			Plan:     plan,
			Pool:     poolMark(p.InRotatePool()),
			Model:    model,
			Priority: p.Priority,
		})
		if !matchesAccountRowFilter(filter, row) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// CmdPool lists accounts currently in the rotate pool (POOL=IN).
func CmdPool() {
	term.Header("amux pool", "rotate set · grouped by priority · am pool add|remove <id>")
	rows := collectPoolRows()
	if len(rows) == 0 {
		term.Warn("Rotate pool empty. am pool add <id>  (see: am accounts)")
		return
	}
	forEachAccountGroup(rows, func(title, _ string, members []accountRow) {
		printDisplaySection(title)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, term.Dim("ID\tKIND\tPRIORITY\tMODEL"))
		for _, r := range members {
			prio := "-"
			if r.Kind != "claude" && r.Kind != "antigravity" {
				prio = fmt.Sprintf("%d", r.Priority)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, r.Kind, prio, r.Model)
		}
		w.Flush()
		term.PanelEnd()
	})
}

func collectPoolRows() []accountRow {
	var rows []accountRow
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
			rows = append(rows, accountRow{
				Group: router.ResolveProfileGroup(tool, p.Plan),
				ID:    pName,
				Kind:  tool,
				Model: "-",
				Pool:  "IN",
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
		rows = append(rows, withAPIProvider(accountRow{
			Group:    router.ResolveAccountGroup(p.ID, p.Type, p.Plan, p.Group),
			ID:       p.ID,
			Kind:     providerKindLabel(p.Type),
			Model:    model,
			Priority: p.Priority,
			Pool:     "IN",
		}))
	}
	return rows
}

// CmdAccountsCmd handles `am accounts [priority <id> <N>]`.
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
			fmt.Println("Usage: amux accounts rm <id>")
			fmt.Println("   or: amux api rm <id>")
			return
		}
		if err := provider.RemoveProvider(provider.DefaultAccountsPath(), args[1]); err != nil {
			fmt.Printf("Error removing provider: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("Removed provider %q from pool\n", args[1])
	case "priority":
		if len(args) < 3 {
			fmt.Println("Usage: amux accounts priority <id> <N>")
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
			fmt.Println("Usage: amux accounts model <id> <model>")
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
			fmt.Println("Usage: am off <id>   (or: am pool remove <id>)")
			return
		}
		if err := provider.SetEnabled(provider.DefaultAccountsPath(), args[1], false); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("off %s — out of rotate (am on %s)\n", args[1], args[1])
	case "on", "enable":
		if len(args) < 2 {
			fmt.Println("Usage: am on <id>   (or: am pool add <id>)")
			return
		}
		if err := provider.SetEnabled(provider.DefaultAccountsPath(), args[1], true); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		proxy.Sync()
		fmt.Printf("on %s — back in rotate\n", args[1])
	default:
		fmt.Println("Usage: am accounts | am accounts rm <id> | am pool add|remove|priority|model")
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
