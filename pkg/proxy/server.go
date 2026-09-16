package proxy

import (
	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/nav"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/bridge"
	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
	"amux-accounts/pkg/usage"
)

type ProxyMode struct {
	mu   sync.Mutex
	mode string // "claude" | "provider"
}

func (m *ProxyMode) Get() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mode == "" {
		return "claude"
	}
	return m.mode
}

func (m *ProxyMode) Set(v string) {
	m.mu.Lock()
	m.mode = v
	m.mu.Unlock()
}

// swappableHandler lets /_am/shutdown swap in a stripped-down passthrough
// handler (see passthrough.go) for a brief grace window before the socket
// actually closes, without needing to rebind the port — the *http.Server
// always points at one of these, and only its target changes.
type swappableHandler struct {
	mu sync.RWMutex
	h  http.Handler
}

func (s *swappableHandler) Set(h http.Handler) {
	s.mu.Lock()
	s.h = h
	s.mu.Unlock()
}

func (s *swappableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.h
	s.mu.RUnlock()
	h.ServeHTTP(w, r)
}

// shutdownGrace is how long /_am/shutdown keeps serving Anthropic-direct
// (see swappableHandler, newPassthroughHandler) after being asked to stop,
// when at least one claude session is still attached — long enough for a
// request already in flight to land somewhere real instead of getting
// connection-refused, short enough that `am proxy down` doesn't hang.
const shutdownGrace = 3 * time.Second

// RunProxy starts the server on the given address, serving Claude Code,
// OpenAI gateway, and administrative endpoints.
func RunProxy(addr, upstream string) error {
	monitor.EnableTermSink()

	if addr == "" {
		addr = ResolveListenAddr("")
	} else {
		addr = normalizeListenAddr(addr)
	}
	if upstream == "" {
		upstream = "https://api.anthropic.com"
	}

	profile.SyncActiveFromSystem("claude")

	rot := NewRotator("claude")
	go rot.PeriodicSnapshot()
	life := NewLifecycle()
	mode := &ProxyMode{}

	adapters, _ := provider.LoadAccounts(provider.DefaultAccountsPath())
	pool := router.NewAccountPoolRouter(adapters)
	if all, err := provider.LoadAllAddressable(provider.DefaultAccountsPath()); err == nil {
		pool.SetDirectory(all)
	}
	if f, err := provider.LoadConfigFile(provider.DefaultAccountsPath()); err == nil && f != nil {
		router.SetWebPolicy(f.WebPolicy)
	}

	// Wire the /btw queue into the bridge so in-flight user notes get
	// injected into the next outgoing LLM request automatically.
	bridge.SetBtwDrainer(GetGlobalBtwQueue().Drain)

	rp, err := newReverseProxy(upstream, rot)
	if err != nil {
		return err
	}

	sw := &swappableHandler{}
	var srv *http.Server
	var authToken string
	if IsPublicBind(addr) {
		token, err := IssueNewAuthToken()
		if err != nil {
			return fmt.Errorf("generate public API key: %w", err)
		}
		authToken = token
		term.LogProxy("public bind: API key %s required for non-loopback requests (see: am proxy token)", authToken)
	}
	handler := newHandler(rot, life, mode, pool, pool, rp, upstream, sw, authToken, func() {
		if srv != nil {
			_ = srv.Close()
		}
		_ = ClearAuthToken()
		StopAuthRateLimiter()
	})
	if authToken != "" {
		handler = requireAuth(authToken, handler)
	}

	sw.Set(handler)
	srv = &http.Server{Addr: addr, Handler: sw}

	// Restore caches from disk
	_ = ctxshrink.GlobalDeduplicator().LoadSnapshot("")
	_ = GlobalReplayCache().LoadSnapshot("")

	// Periodic cache flusher (every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_ = ctxshrink.GlobalDeduplicator().SaveSnapshot("")
			_ = GlobalReplayCache().SaveSnapshot("")
		}
	}()

	// Pre-warm upstream TLS and TCP connections in background so the first prompt
	// experiences zero DNS and TLS handshake latency.
	provider.WarmUpConnections([]string{
		upstream,
		"https://api.anthropic.com",
		"https://api.openai.com",
		"https://generativelanguage.googleapis.com",
	})

	_ = os.MkdirAll(types.BaseDir(), 0o700)
	term.LogProxy("up on %s · active %q", addr, rot.Active())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// hasCallerCredential reports whether the incoming request already carries
// its own Anthropic credential (an API key, or an OAuth-style bearer token
// from the client) rather than needing one injected by the rotator.
// If the token matches proxyToken (the proxy's public amux-<token>), it is treated
// as proxy authentication rather than an upstream Anthropic key.
func hasCallerCredential(r *http.Request, proxyToken string) bool {
	key := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if key != "" {
		// Local Claude/Codex hook uses this fixed placeholder to authenticate
		// against amux. It is never an upstream credential, even on loopback
		// where proxyToken is intentionally empty.
		if key == "am-proxy" {
			return false
		}
		if proxyToken == "" || subtle.ConstantTimeCompare([]byte(key), []byte(proxyToken)) != 1 {
			return true
		}
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		bearer := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if bearer != "" {
			if bearer == "am-proxy" {
				return false
			}
			if proxyToken == "" || subtle.ConstantTimeCompare([]byte(bearer), []byte(proxyToken)) != 1 {
				return true
			}
		}
	}
	return false
}

// newReverseProxy builds the httputil.ReverseProxy that forwards to the real
// Anthropic API (or whatever `upstream` points at), injecting either the
// rotator's OAuth token or a caller-supplied API key.
//
// While the proxy is up, every account decision must stay inside the proxy:
// if the pool has no usable account and the caller didn't bring their own
// credential, the caller (server.go, before invoking this handler) refuses
// the request outright rather than this Director silently falling back to
// the *proxy process's own* ANTHROPIC_API_KEY env var — that key belongs to
// whatever machine is hosting the proxy, not to the caller, and forwarding
// on it would spend someone's real API credits behind their back. Falling
// through to the real Anthropic API is only correct once the proxy itself
// is down (see pkg/hook.SyncLaunchctlEnv / pkg/env.PrintEnvExports), never
// while it's up and simply out of working pool accounts — hence no
// os.Getenv("ANTHROPIC_API_KEY") read anywhere in this Director.
func newReverseProxy(upstream string, rot *Rotator) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream url %s: %w", upstream, err)
	}

	return &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host
			tok := rot.Token()
			if tok != "" {
				r.Header.Set("Authorization", "Bearer "+tok)
				r.Header.Del("X-Api-Key")
			}
			// Maintain anthropic-beta header cleanly: merge without duplicate headers so
			// prompt caching (prompt-caching-2024-07-31) and oauth are always honored by Anthropic.
			curBeta := r.Header.Get("anthropic-beta")
			var betas []string
			if curBeta != "" {
				for _, b := range strings.Split(curBeta, ",") {
					b = strings.TrimSpace(b)
					if b != "" {
						betas = append(betas, b)
					}
				}
			}
			hasOAuth := false
			hasCache := false
			for _, b := range betas {
				if strings.Contains(b, "oauth") {
					hasOAuth = true
				}
				if strings.Contains(b, "prompt-caching") {
					hasCache = true
				}
			}
			if !hasCache {
				betas = append(betas, "prompt-caching-2024-07-31")
			}
			if tok != "" && !hasOAuth {
				betas = append(betas, "oauth-2025-04-20")
			}
			r.Header.Set("anthropic-beta", strings.Join(betas, ","))
			// Scrub internal routing and leak headers before outbound dispatch
			guard.SanitizeOutboundRequest(r)
			// Redact body before it leaves the machine toward Anthropic/upstream.
			redactOutboundBody(r)
		},
		Transport: &dynamicProxyRoundTripper{rot: rot},
		ModifyResponse: func(resp *http.Response) error {
			if !shouldObserveUpstream(resp) {
				return nil
			}
			rot.Observe(resp)
			usage.WrapUsageCapture(resp, rot.Active())
			active := rot.Active()
			if active != "" {
				switch resp.StatusCode {
				case http.StatusOK, http.StatusCreated:
					guard.RecordSuccess(active)
				case http.StatusTooManyRequests:
					retryAfter := guard.ParseRetryAfter(resp.Header)
					guard.GlobalHealth().RecordRateLimit(active, retryAfter)
					guard.GlobalPacer().RecordRateLimit(active, retryAfter)
				case http.StatusUnauthorized, http.StatusForbidden:
					guard.GlobalHealth().RecordAuthError(active, resp.Status)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %v", err)
			http.Error(w, "amux proxy: upstream error", http.StatusBadGateway)
		},
	}, nil
}

type dynamicProxyRoundTripper struct {
	rot *Rotator
}

func (d *dynamicProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var proxyURL string
	if d.rot != nil {
		active := d.rot.Active()
		if active != "" {
			meta := profile.ReadMeta("claude", active)
			if meta.Proxy != "" {
				proxyURL = meta.Proxy
			}
		}
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("AM_EGRESS_PROXY")
	}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		client, err := guard.GetClientForProxy(proxyURL)
		if err != nil {
			log.Printf("guard: egress proxy failed for %q: %v", proxyURL, err)
		} else if client != nil && client.Transport != nil {
			return client.Transport.RoundTrip(req)
		}
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newHandler builds the full HTTP handler serving Claude Code, the OpenAI
// gateway, and the /_am/ admin endpoints.
//
// chatPool serves /v1/chat/completions; toolPool serves Claude/Codex
// /v1/messages failover. Callers may pass the same router for both.
func newHandler(rot *Rotator, life *Lifecycle, mode *ProxyMode, chatPool, toolPool *router.AccountPoolRouter, rp http.Handler, upstream string, sw *swappableHandler, authToken string, shutdown func()) http.Handler {
	if toolPool == nil {
		toolPool = chatPool
	}
	mux := http.NewServeMux()

	// 1. OpenAI Standard Gateway
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		bridge.HandleChatCompletions(w, r, chatPool)
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		bridge.HandleChatCompletions(w, r, chatPool)
	})
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		bridge.HandleOpenAIResponses(w, r, chatPool)
	})
	mux.HandleFunc("/responses", func(w http.ResponseWriter, r *http.Request) {
		bridge.HandleOpenAIResponses(w, r, chatPool)
	})
	mux.HandleFunc("/v1/models", bridge.HandleModels)
	mux.HandleFunc("/models", bridge.HandleModels)

	// 2. Admin / Monitoring Endpoints
	//
	// /_am/* is amux's own control plane (status, rotate, shutdown).
	// It is not a Claude Code / Codex command bus. Slash commands, /model,
	// /compact, /login, MCP, etc. stay in the client; this proxy only
	// speaks the upstream HTTP APIs those clients already use.
	mux.HandleFunc("/_am/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		s := rot.Status()
		s["sessions"] = life.Sessions()
		s["upstream"] = upstream
		s["mode"] = mode.Get()
		s["pool"] = chatPool.Status()
		s["tool_pool"] = toolPool.Status()
		s["guard"] = guard.Status()
		_ = json.NewEncoder(w).Encode(s)
	})

	mux.HandleFunc("/_am/guard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(guard.Status())
	})

	mux.HandleFunc("/_am/guard/reset", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" || target == "all" {
			guard.ResetAll()
		} else {
			guard.GlobalHealth().Reset(target)
			guard.GlobalPacer().Reset(target)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "target": target})
	})

	mux.HandleFunc("/_am/switch", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("to")
		if name == "" {
			http.Error(w, "missing 'to'", http.StatusBadRequest)
			return
		}
		if err := rot.ForceSwitch(name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mode.Set("claude")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"active": rot.Active(), "mode": "claude"})
	})

	mux.HandleFunc("/_am/switch-provider", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.URL.Query().Get("to"))
		if name == "" {
			chatPool.ClearPreferred()
			toolPool.ClearPreferred()
			mode.Set("provider")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"active": "", "mode": "provider", "pin": "cleared"})
			return
		}
		chatPool.SetPreferred(name)
		toolPool.SetPreferred(name)
		// Keep each provider's persisted conversation. A manual provider switch
		// must not burn a new web conversation; adapters reset only on a stale
		// thread or provider-confirmed usage limit.
		mode.Set("provider")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"active": name, "mode": "provider"})
	})

	mux.HandleFunc("/_am/session", func(w http.ResponseWriter, r *http.Request) {
		pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
		if err != nil || pid <= 0 {
			http.Error(w, "invalid or missing 'pid'", http.StatusBadRequest)
			return
		}
		event := r.URL.Query().Get("event")
		if event == "" {
			event = r.URL.Query().Get("op")
		}
		switch event {
		case "start":
			life.AddSession(pid)
		case "end":
			life.EndSession(pid)
		}
		fmt.Fprintf(w, "%d\n", life.Sessions())
	})

	mux.HandleFunc("/_am/pool", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"preferred": chatPool.Preferred(),
			"providers": chatPool.Status(),
			"tool_pool": toolPool.Status(),
		})
	})

	mux.HandleFunc("/_am/btw", HandleBtw)

	// Client workspace map paths (same as `am map show` JSON).
	mux.HandleFunc("/_am/map", handleAmMapBundle)
	mux.HandleFunc("/_am/map/viz", handleAmMapViz)

	mux.HandleFunc("/_am/sync", func(w http.ResponseWriter, r *http.Request) {
		profile.SyncActiveFromSystem("claude")
		rot.RefreshFromDisk()
		rot.EvictDisabledActive()
		if reloaded, err := provider.LoadAccounts(provider.DefaultAccountsPath()); err == nil {
			chatPool.Reload(reloaded)
			if toolPool != chatPool {
				toolPool.Reload(reloaded)
			}
		}
		if f, err := provider.LoadConfigFile(provider.DefaultAccountsPath()); err == nil && f != nil {
			router.SetWebPolicy(f.WebPolicy)
		}
		if all, err := provider.LoadAllAddressable(provider.DefaultAccountsPath()); err == nil {
			chatPool.SetDirectory(all)
			if toolPool != chatPool {
				toolPool.SetDirectory(all)
			}
		}
		fmt.Fprintf(w, "%d\n", len(rot.Names()))
	})

	mux.HandleFunc("/_am/shutdown", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
		go func() {
			if life.Sessions() > 0 {
				if ph, err := newPassthroughHandler(rot, life, upstream, false, nil); err == nil {
					if authToken != "" {
						ph = requireAuth(authToken, ph)
					}
					sw.Set(ph)
				} else {
					log.Printf("amux proxy: grace-drain passthrough unavailable: %v", err)
				}
				time.Sleep(shutdownGrace)
			} else {
				time.Sleep(100 * time.Millisecond)
			}
			_ = ctxshrink.GlobalDeduplicator().SaveSnapshot("")
			_ = GlobalReplayCache().SaveSnapshot("")
			shutdown()
		}()
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleConnectTunnel(w, r)
			return
		}

		path := r.URL.Path

		if strings.HasPrefix(path, "/_am/") {
			withGzip(mux).ServeHTTP(w, r)
			return
		}

		if strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") {
			mux.ServeHTTP(w, r)
			return
		}

		// /v1/models is shared: OpenAI SDKs and Claude Code both hit it.
		// Anthropic clients must see Anthropic's real catalog (so /model and
		// related Claude Code commands work). OpenAI clients keep the local list.
		if path == "/v1/models" || path == "/models" {
			if isAnthropicClient(r) && anthropicUpstreamReady(rot, r, authToken) {
				rp.ServeHTTP(w, r)
				return
			}
			mux.ServeHTTP(w, r)
			return
		}

		// Gemini / Antigravity Gateway
		if path == "/v1beta/models" {
			bridge.HandleGeminiModels(w, r)
			return
		}
		if strings.Contains(path, ":countTokens") {
			bridge.HandleGeminiCountTokens(w, r)
			return
		}
		if strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent") {
			bridge.HandleGeminiGenerateContent(w, r, chatPool)
			return
		}

		// Claude Code /compact, context meter, auto-compact call this.
		// Forward to Anthropic when a Claude credential is usable; otherwise
		// fall back to a local estimate so pool-only mode still answers.
		if strings.HasSuffix(path, "/messages/count_tokens") || strings.HasSuffix(path, "/count_tokens") {
			if anthropicUpstreamReady(rot, r, authToken) {
				rp.ServeHTTP(w, r)
				return
			}
			bridge.HandleClaudeCountTokens(w, r)
			return
		}

		// Anthropic messages (/v1/messages) — Claude Code / tool clients.
		//
		// Same model as attaching an API key to Claude Code:
		//   ANTHROPIC_BASE_URL → this proxy
		//   Claude Code owns tools locally; mid-layer pkg/tools
		//   converts tool defs/calls between Anthropic ↔ OpenAI ↔ Gemini.
		// When a Claude OAuth account is usable and the request includes
		// tools[], reverse-proxy to Anthropic for native tool_use.
		// Provider-pool path also preserves tools via bridge + pkg/tools
		// so OpenAI-compatible backends can drive Claude Code's agent loop.
		if strings.HasSuffix(path, "/messages") {
			body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
			if err != nil {
				http.Error(w, "read request body: "+err.Error(), http.StatusBadRequest)
				return
			}
			if privacy.Enabled {
				if redacted, res := privacy.RedactBytes(body); res.Len() > 0 {
					body = redacted
					privacy.LogHits(r, res, "claude")
				}
				r.Header.Set("X-Amux-Redacted", "1")
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("Content-Length", strconv.Itoa(len(body)))

			xProvider := strings.TrimSpace(r.Header.Get("X-Provider"))
			if xProvider == "" {
				xProvider = strings.TrimSpace(r.Header.Get("x-provider"))
			}

			hasTools := anthropicRequestHasTools(body)
			// Caller-supplied credential only — deliberately excludes this
			// *proxy process's* own ANTHROPIC_API_KEY env var. While the
			// proxy is up, routing must stay inside the pool/rotator; a key
			// sitting in the proxy host's environment is not the caller's
			// to spend, and reading it here would let a fully-dead pool
			// silently escape to the real Anthropic API instead of failing
			// loud (see newReverseProxy's doc comment for the same rule
			// applied to the Director).
			hasAPIKey := hasCallerCredential(r, authToken)
			if authToken != "" {
				if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(r.Header.Get("X-Api-Key"))), []byte(authToken)) == 1 {
					r.Header.Del("X-Api-Key")
				}
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					bearer := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
					if subtle.ConstantTimeCompare([]byte(bearer), []byte(authToken)) == 1 {
						r.Header.Del("Authorization")
					}
				}
			}

			claudeUsable := !rot.ShouldFailoverToProviderPool() &&
				(rot.Token() != "" || rot.ProfileCount() > 0)

			usePool := false
			pool := toolPool
			autoFromClaude := false

			// Explicit X-Provider: pin that pool account (even out of rotate).
			// Claude profile names still use ForceSwitch + reverse-proxy below.
			if xProvider != "" {
				if err := rot.ForceSwitchExplicit(xProvider); err == nil {
					mode.Set("claude")
					rp.ServeHTTP(w, r)
					return
				}
				usePool = true
			}

			var parsedReq *types.ChatRequest
			if rReq, err := bridge.ToChatRequest(body); err == nil {
				parsedReq = rReq
			}

			// Deterministic Replay Cache: identical requests (e.g. repeated prompt, linter, tests)
			// return the exact cached response instantly with 0 tokens and 0 cost.
			rawReplayKey, canReplay := ctxshrink.GlobalReplayCache().ComputeRawHash(body)
			if canReplay {
				if cached, found := ctxshrink.GlobalReplayCache().Get(rawReplayKey); found {
					isStream := false
					if parsedReq != nil {
						isStream = parsedReq.Stream
					}
					term.LogProxy("⚡ Deterministic Replay Cache HIT [key=%s] (0 upstream tokens, saved %d tokens, 0$)",
						rawReplayKey[:8], cached.InputTokens)
					usage.AppendUsageEntry(types.UsageEntry{
						Time:      time.Now(),
						Account:   "replay-cache",
						Output:    cached.OutputTokens,
						CacheRead: cached.InputTokens,
					})
					_ = cached.Serve(w, isStream)
					return
				}
			}

			isWebTask := false
			if parsedReq != nil {
				c := router.ClassifyTask(parsedReq)
				isWebTask = router.IsWebTask(c.Kind)
			}

			switch {
			case usePool:
				// already decided via X-Provider
			case isWebTask && toolPool != nil && !toolPool.ManualPin() && toolPool.HasLivingGroup(router.GroupClaudeWeb, router.GroupChatGPTWeb, router.GroupGeminiWeb):
				usePool = true
			case hasTools && claudeUsable:
				if rot.ProfileCount() > 0 {
					rot.EnsureUsableActive()
				}
				usePool = false
			case mode.Get() == "provider":
				usePool = true
			case hasAPIKey:
				usePool = false
			case rot.ShouldFailoverToProviderPool():
				usePool = toolPool.Len() > 0
				if usePool {
					autoFromClaude = true
					log.Printf("amux: all Claude accounts unavailable — failover to provider pool (API→web)")
				}
			case rot.Token() != "" || rot.ProfileCount() > 0:
				if rot.ProfileCount() > 0 {
					rot.EnsureUsableActive()
				}
				usePool = rot.Token() == "" && !hasAPIKey
			default:
				usePool = toolPool.Len() > 0
			}

			// If session switched account (e.g. rate limit, auto-rotate, failover),
			// compact the transcript so the new account does not pay massive
			// uncached / cache_creation token penalties on stale historical context.
			targetAccount := rot.Active()
			if usePool {
				targetAccount = pool.Preferred()
				if targetAccount == "" {
					targetAccount = "pool"
				}
			}
			if switched, prevAcct := guard.CheckSessionAccountSwitch(r, nil, targetAccount); switched {
				if parsedReq != nil && len(parsedReq.Messages) > 4 {
					compacted := ctxshrink.CompactForAccountSwitchProject(parsedReq.Project(), parsedReq.Messages, 6)
					if len(compacted) < len(parsedReq.Messages) || ctxshrink.EstimateMessagesTokens(compacted) < ctxshrink.EstimateMessagesTokens(parsedReq.Messages) {
						term.LogProxy("session switched (%s → %s): compacting %d turns down to %d to save tokens on cold account",
							prevAcct, targetAccount, len(parsedReq.Messages), len(compacted))
						parsedReq.Messages = compacted
						if newBody, err := tools.MarshalClaudeMessagesRequest(parsedReq, parsedReq.Model); err == nil {
							body = newBody
							r.Body = io.NopCloser(bytes.NewReader(body))
							r.ContentLength = int64(len(body))
							r.Header.Set("Content-Length", strconv.Itoa(len(body)))
						}
					}
				}
			} else {
				// Regular request within same account: deduplicate repeated historical tool outputs
				if parsedReq != nil && len(parsedReq.Messages) > 2 {
					deduped := ctxshrink.GlobalDeduplicator().DeduplicateMessages(parsedReq.Project(), parsedReq.Messages, 2)
					if ctxshrink.EstimateMessagesTokens(deduped) < ctxshrink.EstimateMessagesTokens(parsedReq.Messages) {
						parsedReq.Messages = deduped
						if newBody, err := tools.MarshalClaudeMessagesRequest(parsedReq, parsedReq.Model); err == nil {
							body = newBody
							r.Body = io.NopCloser(bytes.NewReader(body))
							r.ContentLength = int64(len(body))
							r.Header.Set("Content-Length", strconv.Itoa(len(body)))
						}
					}
				}
			}

			if usePool {
				if err := bridge.HandleClaudeMessages(w, r, pool, body); err != nil {
					log.Printf("bridge /v1/messages error: %v", err)
					return
				}
				if autoFromClaude {
					if id := pool.LastUsed(); id != "" {
						pool.SetPreferred(id)
						mode.Set("provider")
						log.Printf("amux: auto-switched active provider → %s (status updated)", id)
					}
				}
				return
			}

			// Nothing usable: no rotator token, no caller-supplied
			// credential, no provider pool to fail over to. Refuse here
			// rather than forwarding — while the proxy is up, account
			// selection must stay inside it, never silently escape to the
			// real Anthropic API on the proxy host's own credentials (or
			// with no credential at all). Only a genuinely stopped proxy
			// (`am proxy down`) should let clients reach Anthropic directly.
			if rot.Token() == "" && !hasAPIKey {
				http.Error(w, "amux proxy: no usable account in pool (all Claude accounts off/expired/rate-limited, "+
					"no provider pool configured) — fix the pool with `am ls` / `am add`, or stop the proxy to fall "+
					"back to the real Anthropic API (`am proxy down`)", http.StatusServiceUnavailable)
				return
			}

			rp.ServeHTTP(w, r)
			return
		}

		// Any other Anthropic (or unknown) path — files, models/{id}, betas,
		// whatever Claude Code adds next — reverse-proxy. Do not invent a
		// matching /_am/* command for each client feature.
		rp.ServeHTTP(w, r)
	})
}

// isAnthropicClient reports whether the caller is speaking Anthropic's API
// (Claude Code, Anthropic SDK) rather than OpenAI's. Same path (/v1/models)
// must not return OpenAI's list to Claude Code.
func isAnthropicClient(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.TrimSpace(r.Header.Get("anthropic-version")) != "" {
		return true
	}
	ua := r.Header.Get("User-Agent")
	if strings.Contains(ua, "Claude") || strings.Contains(ua, "claude-cli") {
		return true
	}
	return strings.Contains(strings.ToLower(ua), "anthropic")
}

// shouldObserveUpstream is true for quota-bearing Anthropic hops
// (POST /v1/messages). GET catalog and count_tokens 429/401 must not
// rotate or quarantine the Claude profile — compact/MCP loops hit those
// constantly.
func shouldObserveUpstream(resp *http.Response) bool {
	if resp == nil || resp.Request == nil {
		return true
	}
	path := resp.Request.URL.Path
	if strings.Contains(path, "count_tokens") {
		return false
	}
	if resp.Request.Method == http.MethodGet {
		return false
	}
	return true
}

// anthropicUpstreamReady is true when this proxy can authenticate an
// Anthropic reverse-proxy hop: caller brought a real key, or the Claude
// rotator has a usable OAuth profile.
func anthropicUpstreamReady(rot *Rotator, r *http.Request, authToken string) bool {
	if hasCallerCredential(r, authToken) {
		return true
	}
	if rot == nil || rot.ShouldFailoverToProviderPool() {
		return false
	}
	if rot.Token() != "" {
		return true
	}
	if rot.ProfileCount() > 0 {
		rot.EnsureUsableActive()
		return rot.Token() != ""
	}
	return false
}

func handleAmMapBundle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	if root == "" {
		root = usage.ProjectForRemoteAddr(r.RemoteAddr)
	}
	if root == "" {
		http.Error(w, "missing project root (use ?root=/path/to/repo or call from proxied client)", http.StatusBadRequest)
		return
	}
	b := nav.Resolve(root)
	if b.IsAmuxRepository() {
		b.PrimaryMap = "docs/AI_CODEBASE_MAP.md (amux tool — in-repo path relative to amux root)"
		b.PrimaryLocate = "docs/ai-locate.yaml"
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	// Narrow git∪touched focus — prefer this over reading full GRAPH.md.
	if mode == "recent" {
		rep, err := nav.RecentFocus(root, "HEAD", false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode":   "recent",
			"paths":  b.AsPathsOnly(),
			"recent": rep,
			"hint":   "Use recent funcs only. Never dump full GRAPH/MODULES into the LLM.",
		})
		return
	}
	// Default: paths-only (token-safe). Agents must not dump full GRAPH.
	// Opt-in dump: ?full=1
	if r.URL.Query().Get("full") == "1" || strings.EqualFold(r.URL.Query().Get("full"), "true") || mode == "full" {
		_ = json.NewEncoder(w).Encode(b)
		return
	}
	_ = json.NewEncoder(w).Encode(b.AsPathsOnly())
}

func handleAmMapViz(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	if root == "" {
		root = usage.ProjectForRemoteAddr(r.RemoteAddr)
	}
	if root == "" {
		http.Error(w, "missing project root (use ?root=/path/to/repo)", http.StatusBadRequest)
		return
	}
	focus := strings.TrimSpace(r.URL.Query().Get("module"))
	scan, g, err := nav.LoadGraphForDir(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	htmlBytes, err := nav.RenderGraphHTML(scan, g, focus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(htmlBytes)
}

// anthropicRequestHasTools reports whether a /v1/messages body includes a
// non-empty tools array (Claude Code agent requests).
func anthropicRequestHasTools(body []byte) bool {
	var wrap struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return false
	}
	return len(wrap.Tools) > 0
}

// redactOutboundBody rewrites r.Body in place, replacing secrets with samples
// before httputil.ReverseProxy dials the upstream. Safe to call repeatedly.
func redactOutboundBody(r *http.Request) {
	if r == nil || r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return
	}
	if r.Header.Get("X-Amux-Redacted") == "1" {
		r.Header.Del("X-Amux-Redacted")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	_ = r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		r.ContentLength = 0
		return
	}
	if privacy.Enabled {
		if redacted, res := privacy.RedactBytes(body); res.Len() > 0 {
			body = redacted
			privacy.LogHits(r, res, "claude")
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))
}
