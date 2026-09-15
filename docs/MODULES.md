# Module inventory — amux

> ~/.am/workspaces/amux/MODULES.md  
> Auto-generated · doc-comment + name heuristic (không LLM)

Mỗi module: số file, mỗi file: số function, mỗi function: signature + mô tả ngắn.

## `pkg/auth` — 3 files · 16 funcs

Keywords: auth, pkg, login, session, jwt

### `pkg/auth/crypto.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 26 | func | `MasterKey` | MasterKey returns the 32-byte AES key, creating and storing one in the system keychain on first use. A transient keychain error (locked, daemon unreachable, ..… |
| 49 | func | `Encrypt` | Encrypt encrypts plain bytes using AES-GCM with the master key. |
| 70 | func | `Decrypt` | Decrypt decrypts encrypted bytes using AES-GCM with the master key. |

### `pkg/auth/keychain.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | func | `KCGet` | KCGet retrieves a password item from macOS Keychain. |
| 29 | func | `KCAccount` | KCAccount reads the existing item's account attribute, if any. |
| 46 | func | `KCSet` | KCSet writes or updates a generic-password item in macOS Keychain. |

### `pkg/auth/token.go` (10 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 28 | type | `ClaudeCreds` | type/class ClaudeCreds |
| 39 | func | `ParseClaudeCreds` | ParseClaudeCreds unmarshals JSON credentials from Keychain or file. |
| 57 | func | `LiveKeychainToken` | LiveKeychainToken reads the OAuth token currently installed on the system. |
| 70 | func | `TokenExpiryNeedsRefresh` | TokenExpiryNeedsRefresh reports whether an access token is expired or close to expiry. |
| 74 | type | `OAuthRefreshResponse` | type/class OAuthRefreshResponse |
| 81 | func | `RefreshClaudeToken` | RefreshClaudeToken exchanges a refresh token for a fresh access token. |
| 104 | func | `postRefreshRequest` | post refresh request |
| 129 | func | `RefreshedCredsJSON` | RefreshedCredsJSON takes a keychain item's original JSON and replaces claudeAiOauth. |
| 161 | func | `acquireRefreshFileLock` | acquire refresh file lock |
| 183 | func | `RefreshLiveClaudeToken` | RefreshLiveClaudeToken attempts to refresh the token currently in Keychain. Synchronized with in-process mutex and cross-process file lock (flock). Also re-che… |

## `pkg/bridge` — 6 files · 62 funcs

Keywords: bridge, pkg

### `pkg/bridge/claude.go` (21 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | func | `poolAccountLabel` | poolAccountLabel returns a label for `am usage`'s account column when a request was served by the provider pool rather than a Claude OAuth profile: the pinned … |
| 31 | type | `AnthropicMessageRequest` | AnthropicMessageRequest represents the request body sent to /v1/messages by Claude Code. |
| 49 | func | `ToChatRequest` | ToChatRequest converts an Anthropic /v1/messages payload into a standardized types.ChatRequest. Tools and tool_use/tool_result blocks are preserved via pkg/too… |
| 114 | func | `expandAnthropicMessage` | expandAnthropicMessage turns one Anthropic message into one or more canonical ChatMessages. tool_use → assistant.ToolCalls; tool_result → role=tool. |
| 231 | func | `flattenAnthropicContent` | flatten anthropic content |
| 296 | func | `toolResultBody` | tool result body |
| 307 | func | `truncateRunes` | truncate runes |
| 316 | func | `HandleClaudeMessages` | HandleClaudeMessages handles an Anthropic /v1/messages HTTP request using the AccountPoolRouter. |
| 446 | func | `EstimateStringTokens` | EstimateStringTokens computes a realistic BPE token count approximation. |
| 451 | func | `EstimateInputTokens` | EstimateInputTokens estimates token usage for a chat request. |
| 457 | func | `EstimateBytesTokens` | EstimateBytesTokens is the exported wrapper for estimateBytesTokens. It produces the same result as EstimateStringTokens(string(b)) without the allocation. |
| 463 | func | `estimateStringTokens` | estimateStringTokens computes a realistic BPE token count approximation for ASCII, multi-byte UTF-8 (Vietnamese, CJK), and symbols. |
| 485 | func | `estimateBytesTokens` | estimateBytesTokens is like estimateStringTokens but avoids the []byte→string allocation for callers that already have a []byte (e.g. json.RawMessage schemas… |
| 508 | func | `estimateInputTokens` | estimate input tokens |
| 533 | func | `beginAnthropicSSE` | begin anthropic sse |
| 566 | func | `writeAnthropicSSEError` | write anthropic sse error |
| 578 | func | `writeAnthropicSSEPing` | write anthropic sse ping |
| 583 | func | `writeAnthropicSSE` | write anthropic sse |
| 783 | func | `mapFinishReasonAnthropic` | map finish reason anthropic |
| 794 | func | `recordPoolUsage` | record pool usage |
| 810 | func | `HandleClaudeCountTokens` | HandleClaudeCountTokens handles Anthropic /v1/messages/count_tokens requests. |

### `pkg/bridge/gemini.go` (16 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 17 | type | `geminiPart` | type/class geminiPart |
| 24 | type | `geminiFuncResponse` | type/class geminiFuncResponse |
| 29 | type | `geminiContent` | type/class geminiContent |
| 34 | type | `geminiToolDeclaration` | type/class geminiToolDeclaration |
| 38 | type | `geminiGenerateRequest` | type/class geminiGenerateRequest |
| 51 | type | `geminiCandidateContent` | type/class geminiCandidateContent |
| 56 | type | `geminiCandidate` | type/class geminiCandidate |
| 62 | type | `geminiGenerateResponse` | type/class geminiGenerateResponse |
| 71 | func | `parseGeminiModelAndStream` | parse gemini model and stream |
| 90 | func | `GeminiBodyToChatRequest` | GeminiBodyToChatRequest parses a Gemini generateContent request body into canonical ChatRequest. |
| 94 | func | `geminiBodyToChatRequest` | gemini body to chat request |
| 218 | func | `HandleGeminiGenerateContent` | HandleGeminiGenerateContent handles Antigravity and Gemini SDK requests. |
| 448 | func | `HandleGeminiCountTokens` | HandleGeminiCountTokens handles /models/...:countTokens requests for Antigravity & Gemini SDKs. |
| 472 | func | `HandleGeminiModels` | HandleGeminiModels lists available models in Google Gemini API format. |
| 481 | type | `geminiModelItem` | type/class geminiModelItem |
| 503 | func | `writeGeminiStreamError` | write gemini stream error |

### `pkg/bridge/headers.go` (10 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | func | `beginSSE` | beginSSE commits an SSE response before provider setup. Browser-backed providers can spend minutes on a challenge or queue before their first token; committing… |
| 35 | func | `commentKeepalive` | comment keepalive |
| 42 | func | `drainStream` | drain stream |
| 54 | func | `recvStreamChunk` | recvStreamChunk waits for the next upstream chunk while emitting keepalives so downstream SSE clients do not treat a quiet generation gap as a dead proxy. |
| 74 | func | `poolSendStreaming` | poolSendStreaming keeps downstream SSE alive while an upstream adapter is still connecting or waiting for its first token. Only handler goroutine writes Respon… |
| 78 | type | `result` | type/class result |
| 114 | func | `SetBtwDrainer` | SetBtwDrainer registers the function used to drain /btw messages before each LLM request. Called once by the proxy server on startup. |
| 121 | func | `injectBtwMessages` | injectBtwMessages appends any pending /btw messages as a note to the last user message in the conversation. Modifies req.Messages in place only when there are … |
| 160 | func | `explicitProviderHeaders` | explicitProviderHeaders reads optional routing overrides: X-Provider — pool account id (works even when removed from rotate pool) X-Model — override reques… |
| 175 | func | `poolSend` | pool send |

### `pkg/bridge/openai.go` (7 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | func | `HandleChatCompletions` | HandleChatCompletions handles standard OpenAI /v1/chat/completions requests from Cursor, Codex, and other OpenAI-shaped clients. Tools are normalized through p… |
| 290 | func | `openaiClientDialect` | openaiClientDialect maps OpenAI-shaped clients onto pool IDE order. /v1/chat/completions is shared; Cursor stays Cursor, Codex UA/originator must not inherit C… |
| 306 | func | `openAIBodyToChatRequest` | openAIBodyToChatRequest parses a Cursor/Codex OpenAI chat.completions body into the canonical ChatRequest (tools use function.parameters on the wire). |
| 351 | func | `openAIContentString` | open ai content string |
| 379 | func | `recordChatUsage` | record chat usage |
| 395 | func | `HandleModels` | HandleModels returns standard models list (Anthropic format if anthropic-version header present, else OpenAI format). |
| 434 | func | `writeOpenAIStreamError` | write open ai stream error |

### `pkg/bridge/requestlog.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | func | `logChatRequest` | log chat request |
| 88 | func | `pickLogOutput` | pick log output |
| 95 | func | `looksWebAccount` | looks web account |
| 101 | func | `requestToolsSummary` | requestToolsSummary picks tool names the model asked to call, plus ok/err. |

### `pkg/bridge/responses.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 19 | func | `HandleOpenAIResponses` | HandleOpenAIResponses handles OpenAI Responses API (/v1/responses) requests used by modern Codex CLI and agent environments. |
| 365 | func | `responsesBodyToChatRequest` | responsesBodyToChatRequest parses an OpenAI Responses API body into a canonical ChatRequest. |
| 440 | func | `parseResponsesInput` | parse responses input |
| 514 | func | `writeResponsesStreamError` | write responses stream error |

## `pkg/browser` — 3 files · 32 funcs

Keywords: browser, pkg

### `pkg/browser/cdp_login.go` (17 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | type | `WebLoginTarget` | WebLoginTarget describes a site to open and which cookie to wait for. |
| 55 | type | `CapturedWebAuth` | CapturedWebAuth is the session cookie plus a full Cookie header for the login host (Cloudflare / ancillary cookies included). |
| 66 | func | `CaptureWebAuthViaBrowser` | CaptureWebAuthViaBrowser opens a dedicated Chromium window (own profile under ~/.am/browser-profiles — NOT the system Chrome profile, NOT Keychain). User log… |
| 157 | func | `RefreshWebAuthFromProfile` | RefreshWebAuthFromProfile opens the existing ~/.am/browser-profiles/<Profile> window briefly and returns session + full Cookie header if already logged in. Doe… |
| 232 | func | `CaptureCookieViaBrowser` | CaptureCookieViaBrowser opens a dedicated Chromium window and returns the session cookie value (see CaptureWebAuthViaBrowser). |
| 243 | func | `cookieHeaderForHost` | cookieHeaderForHost builds a Cookie request header from CDP cookies whose domain matches hostSubstr (e.g. "claude.ai"). Includes Cloudflare / ancillary cookies… |
| 265 | func | `clearChromiumSingletonLocks` | clear chromium singleton locks |
| 273 | func | `pickSessionCookie` | pickSessionCookie returns the session cookie value for target. ChatGPT often splits the token across __Secure-next-auth.session-token.0/.1/.2. |
| 315 | func | `summarizeAuthCookies` | summarize auth cookies |
| 338 | func | `profileDir` | profile dir |
| 350 | func | `findChromiumBinary` | find chromium binary |
| 376 | func | `pickFreePort` | pick free port |
| 385 | func | `debuggerWSURL` | debugger wsurl |
| 406 | type | `cdpCookie` | type/class cdpCookie |
| 412 | func | `cdpListCookies` | cdp list cookies |
| 428 | func | `cdpCallCookies` | cdp call cookies |
| 469 | func | `clearTargetSessionCookies` | clearTargetSessionCookies removes cached cookies and session data so the user is prompted to log in fresh rather than reusing an existing session. |

### `pkg/browser/claude_account.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | type | `ClaudeAccount` | ClaudeAccount is the useful subset of GET https://claude.ai/api/account. |
| 21 | func | `FetchClaudeAccount` | FetchClaudeAccount loads the signed-in account email and plan from claude.ai using a sessionKey (and optional full Cookie header for Cloudflare jars). |
| 64 | func | `parseClaudeAccountPlan` | parse claude account plan |
| 88 | func | `parseClaudeAccountEmail` | parse claude account email |

### `pkg/browser/cookies.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | type | `BrowserInfo` | type/class BrowserInfo |
| 32 | func | `KnownBrowsers` | KnownBrowsers returns browser cookie stores. Firefox is listed first — its moz_cookies.value column is plaintext, so login works without touching macOS Keych… |
| 69 | func | `ExtractCookie` | ExtractCookie finds cookieName for domainFilter. Prefers Firefox (no keychain). Chromium decrypt is attempted next and may fail if the user hasn't granted Safe… |
| 98 | func | `ParseCookieHeader` | ParseCookieHeader pulls one named cookie out of a raw Cookie header ("a=1; b=2") or a single "name=value" paste from DevTools. |
| 118 | func | `readFirefoxCookie` | read firefox cookie |
| 158 | func | `readChromiumCookie` | read chromium cookie |
| 216 | func | `escapeSQLLike` | escape sql like |
| 221 | func | `decryptCookie` | decrypt cookie |
| 248 | type | `ChatGPTSession` | ChatGPTSession is the useful subset of GET /api/auth/session. |
| 258 | func | `FetchChatGPTSession` | FetchChatGPTSession exchanges a next-auth session-token cookie (or any Cookie header that authenticates chatgpt.com) for access/refresh tokens from the web ses… |
| 308 | func | `FetchChatGPTSessionAccessToken` | FetchChatGPTSessionAccessToken keeps the old single-token helper. |

## `pkg/cli` — 3 files · 50 funcs

Keywords: cli, pkg, command

### `pkg/cli/account.go` (8 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | type | `accountRef` | type/class accountRef |
| 19 | func | `cmdToggleAccount` | cmd toggle account |
| 35 | func | `accountToggleHint` | account toggle hint |
| 42 | func | `applyAccountEnabled` | apply account enabled |
| 53 | func | `resolveAccount` | resolve account |
| 69 | func | `lookupClaudeProfile` | lookup claude profile |
| 90 | func | `cmdPool` | cmd pool |
| 119 | func | `cmdPoolSet` | cmd pool set |

### `pkg/cli/cli.go` (31 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 33 | func | `die` | die |
| 38 | func | `usageHelp` | usage help |
| 110 | func | `Run` | Run executes the am CLI command with the given argument list (including program name as args[0]). |
| 521 | func | `proxyThresholdFromArgs` | proxyThresholdFromArgs reads --threshold N from args, else AM_ROTATE_THRESHOLD, else the 95% default. Values may be percent (95) or fraction (0.95). |
| 541 | func | `proxyListenFromArgs` | proxyListenFromArgs returns an explicit listen override from --public, --addr/-b, and/or --port/-p. Empty means "use env / persisted / default". |
| 552 | func | `toolAndName` | tool and name |
| 577 | func | `resolveName` | resolve name |
| 603 | func | `hasProfile` | has profile |
| 612 | func | `isProviderName` | is provider name |
| 625 | func | `cmdAdd` | cmd add |
| 675 | func | `profileName` | profile name |
| 682 | func | `loginHint` | login hint |
| 695 | func | `cmdRm` | cmd rm |
| 730 | func | `cmdLogs` | cmd logs |
| 784 | func | `confirm` | confirm |
| 794 | func | `cmdRename` | cmd rename |
| 825 | func | `orDash` | or dash |
| 835 | func | `cmdRun` | cmdRun execs tool in-place (replacing this process) with env vars pointed at the local rotating proxy, so the tool's own requests get account rotation for free… |
| 901 | func | `cmdSetup` | cmd setup |
| 940 | func | `cmdHookInstall` | cmd hook install |
| 1054 | func | `cmdHookUninstall` | cmd hook uninstall |
| 1125 | func | `cmdHookStatus` | cmd hook status |
| 1211 | func | `cmdHookTool` | cmd hook tool |
| 1248 | func | `plural` | plural |
| 1255 | func | `cmdFeedback` | cmd feedback |
| 1386 | func | `copyExecutable` | copy executable |
| 1423 | type | `versionInfo` | type/class versionInfo |
| 1428 | func | `getInstalledCommit` | get installed commit |
| 1442 | func | `saveInstalledCommit` | save installed commit |
| 1453 | func | `getRemoteHeadCommit` | get remote head commit |
| 1466 | func | `cmdUpdate` | cmd update |

### `pkg/cli/map.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | func | `cmdMap` | cmd map |
| 96 | func | `cmdMapLearn` | cmd map learn |
| 177 | func | `cmdMapRecent` | cmd map recent |
| 230 | func | `cmdMapTouch` | cmd map touch |
| 268 | func | `cmdMapGet` | cmd map get |
| 297 | func | `truncateRunes` | truncate runes |
| 305 | func | `parseLearnJSON` | parse learn json |
| 325 | func | `bytesTrim` | bytes trim |
| 329 | func | `fileExists` | file exists |
| 334 | func | `cmdMapShow` | cmd map show |
| 361 | func | `printPath` | print path |

## `pkg/env` — 1 files · 5 funcs

Keywords: env, pkg

### `pkg/env/env.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | func | `EnvPath` | env path |
| 16 | func | `LoadEnvVars` | load env vars |
| 26 | func | `SaveEnvVars` | save env vars |
| 32 | func | `ShellQuote` | shell quote |
| 47 | func | `PrintEnvExports` | PrintEnvExports outputs shell export lines for eval "$(am env)". When the proxy is up, exports the same pair Claude Code expects for a custom Anthropic gateway… |

## `pkg/guard` — 6 files · 54 funcs

Keywords: guard, pkg

### `pkg/guard/affinity.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 18 | type | `affinityEntry` | type/class affinityEntry |
| 25 | type | `SessionAffinity` | SessionAffinity pins conversations to a single account to avoid ping-ponging between multiple accounts during multi-turn developer workflows. |
| 33 | func | `NewSessionAffinity` | NewSessionAffinity creates a new SessionAffinity manager. |
| 49 | func | `ExtractSessionKey` | ExtractSessionKey extracts a consistent session identifier from an HTTP request or ChatRequest. Looks at: 1. HTTP Headers: X-Session-Id, Session-Id, X-Conversa… |
| 97 | method | `GetPinned` | GetPinned returns the pinned account ID for this session key if active and not expired. |
| 115 | method | `Pin` | Pin records or refreshes the binding between a session key and an account ID. |
| 129 | method | `Unpin` | Unpin removes the session binding (e.g. after a rate-limit or failover). |
| 139 | method | `UnpinAccount` | UnpinAccount removes all sessions pinned to an account that went offline or into quarantine. |
| 153 | method | `ActivePinsCount` | ActivePinsCount returns the number of active pinned sessions. |

### `pkg/guard/guard.go` (12 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 18 | func | `GlobalHealth` | GlobalHealth returns the singleton health tracker instance. |
| 23 | func | `GlobalPacer` | GlobalPacer returns the singleton traffic pacer instance. |
| 28 | func | `GlobalAffinity` | GlobalAffinity returns the singleton session affinity instance. |
| 33 | func | `Sanitize` | Sanitize scrubs sensitive internal headers before dispatching outbound requests. |
| 38 | func | `Pace` | Pace coordinates request cadence and enforces backoff delay. |
| 43 | func | `RecordSuccess` | RecordSuccess marks a successful turn for the account in both health and pacer. |
| 49 | func | `RecordError` | RecordError updates health score, applies backoff, and may quarantine the account. |
| 60 | func | `ResetAll` | ResetAll resets all tracking state across health, pacer, and affinity. |
| 66 | func | `IsQuarantined` | IsQuarantined checks whether an account is quarantined from active rotation. |
| 71 | func | `GetAffinityAccount` | GetAffinityAccount resolves any sticky account bound to this request or context. |
| 80 | func | `PinSession` | PinSession binds a session to an account for subsequent conversation turns. |
| 88 | func | `Status` | Status returns a serializable snapshot of the anti-ban protection layer status. |

### `pkg/guard/health.go` (16 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 30 | func | `classifyError` | classify error |
| 84 | type | `HealthReport` | HealthReport summarizes the current health of an account. |
| 97 | type | `accountHealthEntry` | type/class accountHealthEntry |
| 110 | type | `HealthTracker` | HealthTracker monitors account health metrics and enforces predictive quarantines. |
| 116 | func | `NewHealthTracker` | NewHealthTracker creates a new HealthTracker instance. |
| 122 | method | `getOrCreateLocked` | get or create locked |
| 134 | method | `RecordSuccess` | RecordSuccess rewards the account by recovering score and clearing consecutive error tallies. |
| 152 | method | `RecordRateLimit` | RecordRateLimit deducts points and quarantines if repeated. |
| 180 | method | `RecordAuthError` | RecordAuthError deducts heavily. If 2 consecutive auth errors happen (e.g. 401/403 or session revoked), the account is immediately quarantined to stop automate… |
| 202 | method | `RecordServerError` | RecordServerError registers 5xx or connection drops. |
| 221 | method | `RecordError` | RecordError automatically categorizes an error and logs health impact. |
| 243 | method | `IsQuarantined` | IsQuarantined reports whether an account is quarantined, remaining time, and the reason. |
| 270 | method | `GetReport` | GetReport returns a snapshot of an account's health metrics. |
| 308 | method | `GetAllReports` | GetAllReports returns health summaries for all tracked accounts. |
| 320 | method | `Reset` | Reset clears health issues for an account (e.g. after manual credential refresh). |
| 327 | method | `ResetAll` | ResetAll clears health tracking for all accounts. |

### `pkg/guard/pacer.go` (12 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | func | `ParseRetryAfter` | ParseRetryAfter parses standard HTTP Retry-After header (seconds or RFC1123). |
| 45 | type | `PacerConfig` | PacerConfig controls the pacing behavior. |
| 59 | func | `DefaultPacerConfig` | DefaultPacerConfig provides safe production defaults. |
| 69 | type | `backoffState` | type/class backoffState |
| 75 | type | `Pacer` | Pacer coordinates request timing across accounts to eliminate bot-like burst patterns. |
| 83 | func | `NewPacer` | NewPacer creates a new request pacer with the given configuration. |
| 105 | method | `InBackoff` | InBackoff checks if an account is currently cooling down due to rate limits. |
| 122 | method | `RecordRateLimit` | RecordRateLimit registers a rate-limit event for an account and computes exponential backoff. |
| 159 | method | `ClearBackoff` | ClearBackoff clears any backoff for an account upon successful turn. |
| 167 | method | `Pace` | Pace waits the necessary duration to maintain healthy cadence before dispatching. For web sessions (isWeb == true), adds humanized random micro-jitter. |
| 213 | method | `Reset` | Reset clears pacing and backoff for a specific account. |
| 221 | method | `ResetAll` | ResetAll resets all accounts state in the pacer. |

### `pkg/guard/proxy_egress.go` (2 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 19 | func | `NewProxyTransport` | NewProxyTransport builds an http.Transport configured to route all traffic through the specified proxyURL (supports http://, https://, socks5://). |
| 43 | func | `GetClientForProxy` | GetClientForProxy returns a cached, shared *http.Client configured with the given egress proxy to maximize TCP/TLS connection reuse. |

### `pkg/guard/sanitizer.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 42 | func | `SanitizeOutboundHeaders` | SanitizeOutboundHeaders scrubs internal routing, proxy tags, and client IP leak headers before the HTTP request is dispatched to any upstream LLM service. It p… |
| 59 | func | `SanitizeOutboundRequest` | SanitizeOutboundRequest performs header sanitization on an outbound http.Request. |
| 68 | func | `EnsureSafeUserAgent` | EnsureSafeUserAgent ensures the User-Agent header looks natural and does not advertise proxy / automated scrapers if empty. |

## `pkg/hook` — 5 files · 62 funcs

Keywords: hook, pkg

### `pkg/hook/autoupdate.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 27 | func | `LaunchAgentPath` | LaunchAgentPath returns the macOS LaunchAgent plist path for amux auto-update. |
| 36 | type | `AutoUpdateConfig` | AutoUpdateConfig records the current auto-update state and configuration. |
| 43 | func | `AutoUpdateConfigFile` | AutoUpdateConfigFile returns the path to ~/.am/autoupdate.json. |
| 48 | func | `IsAutoUpdateEnabled` | IsAutoUpdateEnabled reports whether auto-update is currently configured and active. |
| 64 | func | `SetupAutoUpdate` | SetupAutoUpdate enables or disables automatic updates via macOS LaunchAgent. |

### `pkg/hook/feedback.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 29 | func | `InstallSlashCommand` | InstallSlashCommand writes a command file under ~/.claude/commands/am/. |

### `pkg/hook/hook.go` (42 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 30 | func | `CanonicalTool` | CanonicalTool normalizes user input into one of: claude, agy, codex, cursor. Returns empty string if unknown. |
| 45 | func | `ClaudeAvailable` | claude available |
| 54 | func | `GeminiAvailable` | gemini available |
| 63 | func | `CodexAvailable` | codex available |
| 72 | func | `CursorAvailable` | cursor available |
| 81 | func | `ClaudeSettingsPath` | claude settings path |
| 86 | func | `LoadClaudeSettings` | load claude settings |
| 98 | func | `SaveClaudeSettings` | save claude settings |
| 110 | func | `AddHook` | AddHook appends our command to settings["hooks"][event], leaving any other entries (and other events) untouched. A previous version of our own entry (however i… |
| 134 | func | `isOurHookCommand` | HookEntryIsOurs recognises our own hook entries even across the command formats we've used historically: a bare "am proxy up"/"am proxy down", or (older instal… |
| 144 | func | `HookEntryIsOurs` | hook entry is ours |
| 173 | func | `HookInstall` | HookInstall wires SessionStart/SessionEnd/Stop hooks to the currently running `am` binary's resolved absolute path (quoted, so it survives paths with spaces) �… |
| 186 | func | `HookUninstall` | hook uninstall |
| 221 | func | `HookInstalled` | hook installed |
| 238 | func | `InstallStatusLine` | InstallStatusLine writes Claude Code's statusLine command. Does not overwrite a custom statusline the user already set (unless it is ours). |
| 254 | func | `IsOurStatusLine` | is our status line |
| 263 | func | `statusLineCommandIsOurs` | status line command is ours |
| 270 | func | `InstalledEvents` | InstalledEvents returns the hook events we currently own an entry in, e.g. ["SessionStart", "SessionEnd", "Stop"]. |
| 302 | func | `SyncClaudeSettingsEnv` | SyncClaudeSettingsEnv mirrors the proxy's reachability into ~/.claude/settings.json's "env" block, which Claude Code reads at the start of every new session �… |
| 326 | func | `ShellRC` | ShellRC returns the user's shell rc file for zsh/bash, or "" if the shell isn't one we know how to wire automatically. |
| 339 | func | `RCHasLine` | RCHasLine reports whether path already contains line. |
| 345 | func | `AppendLine` | AppendLine appends text to path, creating it if necessary. |
| 355 | func | `GeminiConfigDir` | gemini config dir |
| 360 | func | `GeminiHooksPath` | gemini hooks path |
| 364 | func | `LoadGeminiHooks` | load gemini hooks |
| 376 | func | `SaveGeminiHooks` | save gemini hooks |
| 389 | func | `GeminiHookInstall` | gemini hook install |
| 424 | func | `GeminiHookUninstall` | gemini hook uninstall |
| 438 | func | `GeminiHookInstalled` | gemini hook installed |
| 444 | func | `GeminiInstalledEvents` | gemini installed events |
| 458 | func | `CodexHooksPath` | codex hooks path |
| 463 | func | `LoadCodexHooks` | load codex hooks |
| 475 | func | `SaveCodexHooks` | save codex hooks |
| 484 | func | `CodexHookInstall` | codex hook install |
| 503 | func | `CodexHookUninstall` | codex hook uninstall |
| 539 | func | `CodexHookInstalled` | codex hook installed |
| 554 | func | `CursorHooksPath` | cursor hooks path |
| 559 | func | `LoadCursorHooks` | load cursor hooks |
| 571 | func | `SaveCursorHooks` | save cursor hooks |
| 580 | func | `CursorHookInstall` | cursor hook install |
| 613 | func | `CursorHookUninstall` | cursor hook uninstall |
| 642 | func | `CursorHookInstalled` | cursor hook installed |

### `pkg/hook/launchctl.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 12 | func | `LaunchctlSetenv` | LaunchctlSetenv mirrors a var into the macOS GUI session (launchctl setenv), so apps launched outside any shell — Dock icons, IDE integrations, editor extens… |
| 20 | func | `LaunchctlUnsetenv` | LaunchctlUnsetenv removes a var set via LaunchctlSetenv. No-op off darwin. |
| 37 | func | `SyncLaunchctlEnv` | SyncLaunchctlEnv mirrors the proxy's current reachability into the launchctl session env, so GUI-launched processes (which never source shell rc, see HookInsta… |

### `pkg/hook/limits_status.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | func | `AGYSettingsPath` | agy settings path |
| 21 | func | `LoadAGYSettings` | load agy settings |
| 33 | func | `SaveAGYSettings` | save agy settings |
| 42 | func | `installAGYStatusLine` | install agy status line |
| 48 | func | `uninstallAGYStatusLine` | uninstall agy status line |
| 57 | func | `CodexConfigPath` | codex config path |
| 62 | func | `installCodexStatusLine` | install codex status line |
| 78 | func | `uninstallCodexStatusLine` | uninstall codex status line |
| 91 | func | `ensureCodexStatusLine` | ensure codex status line |
| 113 | func | `isOurCodexStatusLine` | is our codex status line |
| 125 | func | `removeOurCodexStatusLine` | remove our codex status line |

## `pkg/live` — 0 files · 0 funcs

Keywords: live, pkg

*(không có source file được parse)*

## `pkg/monitor` — 3 files · 35 funcs

Keywords: monitor, pkg

### `pkg/monitor/error_diag.go` (23 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | func | `errorsLogPath` | errors log path |
| 17 | func | `legacyLogPath` | legacy log path |
| 18 | func | `statsPath` | stats path |
| 23 | type | `ErrorDiagnostic` | ErrorDiagnostic captures the complete request context and upstream response for a failed turn. It provides root-cause diagnostic information for debugging and … |
| 42 | type | `RequestMetrics` | RequestMetrics stores lightweight in-memory and on-disk counters so the CLI (e.g. `am logs`) and TUI dashboard can display statistics without reading large fil… |
| 63 | func | `SetDiagnosticAllBodies` | SetDiagnosticAllBodies enables writing all payloads (not just errors) to disk. Default is false to prevent disk and memory bloat. |
| 70 | func | `SetLogAllBodies` | SetLogAllBodies is an alias for SetDiagnosticAllBodies. |
| 75 | func | `ResetRequestMetrics` | ResetRequestMetrics clears in-memory cached metrics (useful for testing). |
| 82 | func | `ResetStats` | ResetStats is an alias for ResetRequestMetrics. |
| 87 | func | `GetRequestMetrics` | GetRequestMetrics returns the current aggregate counts of requests and errors. |
| 99 | func | `GetLogStats` | GetLogStats is an alias for GetRequestMetrics. |
| 103 | func | `loadMetricsDisk` | load metrics disk |
| 112 | func | `saveMetricsDisk` | save metrics disk |
| 122 | func | `RecordErrorDiagnostic` | RecordErrorDiagnostic updates aggregate counters and ONLY writes the full turn to errors.log if an error occurred (or if diagnosticAll is explicitly enabled). |
| 178 | func | `AppendFullIO` | AppendFullIO is an alias for RecordErrorDiagnostic. |
| 183 | func | `GetLatestErrorLog` | GetLatestErrorLog retrieves the most recent error block from errors.log (or amux.log). |
| 229 | func | `maybeAutoPrune` | maybe auto prune |
| 241 | func | `PruneLogs` | PruneLogs deletes log entries older than olderThan (e.g. 7 days) from errors.log, amux.log, requests.log, and events.log. |
| 336 | func | `parseBlockTimestamp` | parse block timestamp |
| 358 | func | `fileExists` | file exists |
| 363 | func | `formatDiagnosticBlock` | format diagnostic block |
| 407 | func | `formatMessages` | format messages |
| 446 | func | `dash` | dash |

### `pkg/monitor/sink.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 7 | func | `EnableTermSink` | EnableTermSink wires term.Log → events.log so realtime proxy/CLI events are persisted for `amux logs` and monitoring. |

### `pkg/monitor/store.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 26 | func | `eventsPath` | events path |
| 27 | func | `requestsPath` | requests path |
| 30 | func | `AppendEvent` | AppendEvent writes one tagged event to events.log. |
| 36 | func | `AppendRequest` | AppendRequest writes one chat I/O record to requests.log. |
| 46 | func | `TruncateRunes` | TruncateRunes shortens s to at most n runes with an ellipsis. |
| 55 | func | `LastUserText` | LastUserText picks a preview from chat messages (last user / tool content). |
| 71 | func | `appendJSONL` | append jsonl |
| 88 | func | `trimJSONL` | trim jsonl |
| 111 | func | `LoadEvents` | LoadEvents returns recent events, newest last. tagFilter empty = all; otherwise case-insensitive substring match on tag or message. |
| 135 | func | `LoadRequests` | LoadRequests returns recent request records, newest last. |
| 161 | func | `readJSONL` | read jsonl |

## `pkg/nav` — 7 files · 84 funcs

Keywords: nav, pkg

### `pkg/nav/annotations.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | type | `FuncAnnotation` | FuncAnnotation is an AI/human refinement of a function after deeper reading. Survives am map update (re-scan overlays these summaries). |
| 26 | type | `AnnotationStore` | AnnotationStore is persisted at ~/.am/workspaces/<name>/annotations.json. |
| 32 | func | `annotationsPath` | annotations path |
| 37 | func | `LoadAnnotations` | LoadAnnotations reads the annotation store (empty if missing). |
| 57 | func | `SaveAnnotations` | SaveAnnotations writes the store atomically-ish. |
| 70 | func | `LearnFuncs` | LearnFuncs upserts annotations then regenerates map so MODULES.md reflects them. |
| 119 | func | `upsertAnnotation` | upsert annotation |
| 132 | func | `ApplyAnnotations` | ApplyAnnotations overlays learned summaries onto scan results. |
| 161 | func | `annKey` | ann key |

### `pkg/nav/focus.go` (16 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 20 | type | `FocusState` | FocusState remembers files/funcs the agent already opened this session. Cheap machine state — not sent to LLM unless agent asks. |
| 28 | type | `FocusFuncRef` | type/class FocusFuncRef |
| 34 | type | `FocusHit` | FocusHit is one function in the narrow check/update set. |
| 46 | type | `FocusReport` | FocusReport is the only payload agents should read when checking/updating map. |
| 55 | func | `LoadFocus` | LoadFocus / SaveFocus persist session touch list under workspace. |
| 74 | func | `SaveFocus` | save focus |
| 88 | func | `NoteTouch` | NoteTouch records that the agent opened these files/funcs (no LLM, no full map). |
| 121 | func | `upsertFocusFunc` | upsert focus func |
| 134 | func | `RecentFocus` | RecentFocus builds a narrow report: git-changed funcs ∪ session focus. Does NOT walk whole MODULES.md / whole repo into the agent context — machine filters… |
| 264 | func | `LookupFunc` | LookupFunc returns one function summary from disk scan + annotations (no full map dump). |
| 308 | func | `gitChangedSourceFiles` | git changed source files |
| 340 | type | `lineRange` | type/class lineRange |
| 342 | func | `gitDiffHunks` | git diff hunks |
| 354 | func | `parseDiffHunks` | parse diff hunks |
| 397 | func | `lineInRanges` | line in ranges |
| 410 | func | `looksLikeSource` | looks like source |

### `pkg/nav/generate.go` (10 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | func | `InitScaffold` | InitScaffold scans the project and writes a real map into ~/.am/workspaces/<name>/. force=true regenerates map+locate. |
| 19 | func | `GenerateMap` | GenerateMap always (re)builds navigation files from a local filesystem scan. Does not call any LLM — 0 API tokens. |
| 82 | func | `publishAmuxInventory` | publishAmuxInventory copies scan inventory into docs/ for the amux tool repo (does not overwrite hand-curated AI_CODEBASE_MAP.md or ai-locate.yaml). |
| 106 | func | `UpdateMap` | UpdateMap regenerates map from current tree (same as init --force). |
| 111 | func | `EnsureMapIfMissing` | EnsureMapIfMissing creates a map on first touch when none exists. |
| 123 | func | `renderAgentsMD` | render agents md |
| 142 | func | `renderMapMD` | render map md |
| 192 | func | `renderModulesMD` | render modules md |
| 226 | func | `renderLocateYAML` | render locate yaml |
| 332 | func | `yamlBareOrQuote` | yaml bare or quote |

### `pkg/nav/gitroot.go` (2 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 9 | func | `execGitRoot` | exec git root |
| 25 | func | `gitRootOrSelf` | git root or self |

### `pkg/nav/inventory.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 17 | type | `FuncHit` | FuncHit is one function/method/class extracted from source. |
| 26 | type | `FileHit` | FileHit is one source file inside a module. |
| 33 | func | `enrichModuleInventory` | enrichModuleInventory fills Files / counts for a module directory. |
| 101 | func | `isTestFileName` | is test file name |
| 111 | func | `extractFuncHits` | extractFuncHits parses functions with line, signature, and summary. |
| 227 | func | `docCommentAbove` | doc comment above |
| 273 | func | `summarizeFromName` | summarize from name |
| 304 | func | `splitIdent` | split ident |
| 329 | func | `truncate` | truncate |

### `pkg/nav/nav.go` (20 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | type | `Bundle` | Bundle describes where an AI agent should look for navigation in a client repo. |
| 41 | func | `GitRoot` | GitRoot returns the git toplevel for start, or start if not in a repo. |
| 59 | func | `WorkspaceName` | WorkspaceName is ~/.am/workspaces/<name>/ folder for this project root. Prefer clean project basename; on collision with another root, append -hash4. |
| 84 | func | `shortHash` | short hash |
| 89 | func | `sanitizeSlug` | sanitize slug |
| 107 | func | `WorkspacesRoot` | WorkspacesRoot is ~/.am/workspaces (or $AMUX_HOME/workspaces). |
| 112 | func | `WorkspaceDir` | WorkspaceDir returns ~/.am/workspaces/<name>/. |
| 116 | func | `amBaseDir` | am base dir |
| 129 | func | `Resolve` | Resolve finds navigation files for a client project (repo root). Priority: ~/.am/workspaces/<name>/ → repo docs/ → repo AGENTS.md. |
| 157 | func | `firstExisting` | first existing |
| 170 | method | `HasProjectMap` | HasProjectMap reports whether the project has any navigation artifact. |
| 175 | method | `IsAmuxRepository` | IsAmuxRepository is true when this root is the amux tool repo itself. |
| 188 | func | `EnsureWorkspace` | EnsureWorkspace writes project_root.txt and mirrors any in-repo docs into ~/.am/workspaces/<name>/. |
| 211 | func | `EnsureStoreMirror` | EnsureStoreMirror is kept as an alias for older call sites. |
| 213 | func | `fileExists` | file exists |
| 218 | func | `copyFile` | copy file |
| 227 | func | `WalkWorkspaces` | WalkWorkspaces lists known workspace folders under ~/.am/workspaces/. |
| 249 | func | `StoreID` | Deprecated aliases. |
| 250 | func | `ProjectStoreDir` | project store dir |
| 251 | func | `WalkStoreProjects` | walk store projects |

### `pkg/nav/scan.go` (18 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | type | `ScanResult` | ScanResult is a local filesystem snapshot used to generate maps (0 LLM tokens). |
| 33 | type | `ModuleHit` | ModuleHit is one navigable package/dir. |
| 46 | func | `ScanProject` | ScanProject walks root (bounded) and classifies stack + modules. |
| 67 | func | `detectStack` | detect stack |
| 123 | func | `findEntries` | find entries |
| 159 | func | `discoverModules` | discover modules |
| 229 | func | `isLeafPackage` | is leaf package |
| 246 | func | `looksLikeCodeDir` | looks like code dir |
| 272 | func | `matchLangFile` | match lang file |
| 285 | func | `moduleFromDir` | module from dir |
| 314 | func | `pickReadFirst` | pick read first |
| 354 | func | `pickTests` | pick tests |
| 377 | func | `discoverTestRoots` | discover test roots |
| 388 | func | `keywordsFor` | keywords for |
| 419 | func | `guessSymbols` | guess symbols |
| 437 | func | `extractSymbols` | extract symbols |
| 495 | func | `goFuncName` | go func name |
| 510 | func | `appendUnique` | append unique |

## `pkg/privacy` — 1 files · 20 funcs

Keywords: privacy, pkg

### `pkg/privacy/redact.go` (20 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 24 | type | `Hit` | Hit describes one redaction. Original secret is NEVER stored — only kind + sample. |
| 31 | type | `Result` | Result is the outcome of a redact pass. |
| 40 | method | `Len` | Len returns total replacement occurrences across kinds. |
| 49 | method | `Kinds` | Kinds returns unique kind labels in encounter order. |
| 58 | method | `Summary` | Summary is a short Live Flow preview (no secrets). |
| 69 | type | `rule` | type/class rule |
| 160 | func | `init` | initialize |
| 749 | func | `isNonPublicIPv4` | is non public i pv4 |
| 768 | func | `digitsOnly` | digits only |
| 778 | func | `luhnOK` | luhn ok |
| 795 | func | `isSample` | is sample |
| 819 | func | `RedactString` | RedactString replaces sensitive spans with sample placeholders. |
| 860 | func | `RedactBytes` | RedactBytes redacts a raw request body (JSON or plain text). JSON is walked as decoded strings then re-marshaled — never rewrite raw bytes, or valid escapes … |
| 875 | func | `redactJSON` | redact json |
| 887 | func | `skipJSONRedactKey` | skip json redact key |
| 901 | func | `redactAny` | redact any |
| 950 | func | `RedactChatRequest` | RedactChatRequest mutates message/tool text in place. |
| 990 | func | `LogHits` | LogHits writes a Live Flow row describing redactions (kinds + samples only). |
| 1018 | func | `truncate` | truncate |
| 1027 | func | `MergeResults` | MergeResults combines multiple redact results. |

## `pkg/profile` — 2 files · 62 funcs

Keywords: profile, pkg

### `pkg/profile/manager.go` (41 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | func | `ProfileDir` | profile dir |
| 27 | func | `BundlePath` | bundle path |
| 31 | func | `MetaPath` | meta path |
| 35 | func | `ActivePath` | active path |
| 39 | func | `ReadActivePointer` | read active pointer |
| 44 | func | `WriteActivePointer` | write active pointer |
| 63 | func | `IDPrefixForTool` | IDPrefixForTool returns the unified-ID prefix for a Profile-system tool (e.g. "claude" -> "claude:code"). Exported so callers like cli.toolAndName can recogniz… |
| 70 | func | `ListProfiles` | list profiles |
| 99 | func | `SanitizeName` | sanitize name |
| 106 | func | `MatchProfileByAccount` | match profile by account |
| 118 | func | `ProfileNameForAccount` | profile name for account |
| 122 | func | `ConfigPath` | config path |
| 124 | func | `LoadConfig` | load config |
| 144 | func | `SaveConfig` | save config |
| 150 | func | `ToolNames` | tool names |
| 159 | func | `LookupToolSpec` | lookup tool spec |
| 165 | func | `ToolSpec` | tool spec |
| 175 | func | `DetectAccount` | DetectAccount reads the logged-in email from configured files or keychain. |
| 219 | func | `jsonDottedPath` | json dotted path |
| 237 | func | `parseJWTEmail` | parse jwt email |
| 260 | func | `DetectPlan` | DetectPlan checks whether a tool's current auth state has an active paid subscription or is free tier. |
| 309 | func | `parseJWTClaim` | parse jwt claim |
| 341 | func | `SnapshotArtifact` | snapshot artifact |
| 366 | func | `ApplyEntry` | apply entry |
| 383 | func | `LoadProfileEntries` | load profile entries |
| 396 | func | `unpackEntries` | unpack entries |
| 424 | func | `WriteBundle` | write bundle |
| 463 | func | `UpdateProfileEntry` | update profile entry |
| 477 | func | `LoadClaudeToken` | load claude token |
| 489 | func | `InstallActiveProfile` | install active profile |
| 534 | func | `ReadMeta` | read meta |
| 542 | func | `WriteMeta` | WriteMeta persists profile metadata (name/account/disabled/…). |
| 554 | func | `SetDisabled` | SetDisabled marks a profile off (true) or on (false). Off profiles are skipped by auto-rotate and rejected by `am sw` until turned back on. |
| 570 | func | `IsDisabled` | IsDisabled reports whether the named profile is turned off. |
| 574 | func | `WriteFileAtomic` | write file atomic |
| 586 | func | `CmdSave` | cmd save |
| 640 | func | `SaveActiveProfile` | save active profile |
| 644 | func | `CmdUse` | cmd use |
| 697 | func | `SyncActiveFromSystem` | sync active from system |
| 718 | func | `PrintLiveLogins` | print live logins |
| 739 | func | `SaveDirectProfile` | SaveDirectProfile saves an in-memory credential payload as a managed profile. |

### `pkg/profile/transfer.go` (21 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 28 | type | `PortableProfile` | type/class PortableProfile |
| 36 | type | `PortableBundle` | type/class PortableBundle |
| 42 | func | `DeriveKey` | derive key |
| 46 | func | `CollectBundle` | collect bundle |
| 76 | func | `SealBundle` | SealBundle gzips + AES-256-GCM encrypts the bundle. When saltInline is true the key is passphrase-derived and a fresh salt is prepended; otherwise the caller's… |
| 121 | func | `OpenBundle` | OpenBundle decrypts a sealed blob. When saltInline the key is the raw passphrase and the salt is read from the blob; otherwise key is used directly. |
| 174 | func | `BackupDir` | backup dir |
| 175 | func | `TrashDir` | trash dir |
| 179 | func | `AutoBackup` | AutoBackup writes an encrypted snapshot of every profile across all tools to ~/.am/backups/, keyed by the machine's master key. Keeps the 20 most recent. |
| 201 | func | `PruneBackups` | prune backups |
| 216 | func | `CmdRestoreBackup` | CmdRestoreBackup re-imports the most recent auto-backup (or one named by substring). |
| 251 | func | `DefaultExportPath` | DefaultExportPath names the file `am export` writes when -o isn't given. |
| 263 | func | `ReadPassphrase` | read passphrase |
| 285 | func | `CmdExport` | cmd export |
| 349 | func | `CmdImport` | cmd import |
| 402 | func | `ReadExportBlob` | read export blob |
| 414 | func | `MergeBundle` | merge bundle |
| 441 | func | `UniqueProfileName` | unique profile name |
| 458 | func | `TrashProfile` | trash profile |
| 474 | func | `RestoreProfile` | restore profile |
| 479 | type | `item` | type/class item |

## `pkg/provider` — 15 files · 187 funcs

Keywords: provider, pkg

### `pkg/provider/agy.go` (13 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 29 | type | `agyCredentials` | type/class agyCredentials |
| 40 | type | `AntigravityAdapter` | AntigravityAdapter connects amux to Google Antigravity / Gemini via OAuth. |
| 52 | method | `ID` | id |
| 53 | method | `Priority` | priority |
| 54 | method | `Group` | group |
| 65 | method | `SupportsTools` | SupportsTools is true: AGY supports full function calling and tool execution. |
| 67 | method | `client` | client |
| 75 | func | `AGYCredentialsPath` | AGYCredentialsPath returns the path to Antigravity CLI's credentials file. |
| 84 | func | `AGYAuthAvailable` | AGYAuthAvailable reports whether Antigravity credentials or Keychain entry exist. |
| 89 | func | `loadAGYCredentials` | load agy credentials |
| 110 | func | `saveAGYCredentials` | save agy credentials |
| 124 | method | `ensureAccessToken` | ensure access token |
| 192 | method | `SendMessageStream` | send message stream |

### `pkg/provider/chatgpt_sentinel.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 26 | func | `chatgptAccountIDFromJWT` | chatgptAccountIDFromJWT extracts chatgpt_account_id from an access-token JWT without verifying the signature (same trust model as the rest of the web adapter �… |
| 49 | type | `chatgptSentinelTokens` | type/class chatgptSentinelTokens |
| 61 | func | `fetchChatGPTSentinel` | fetchChatGPTSentinel calls /sentinel/chat-requirements and solves the SHA3-512 proof-of-work challenge. Without these headers ChatGPT returns 403 "Unusual acti… |
| 109 | func | `solveChatGPTPoW` | solve chat gpt po w |
| 150 | func | `setChatGPTWebHeaders` | set chat gpt web headers |

### `pkg/provider/chatgpt_web.go` (12 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | type | `ChatGPTWebAdapter` | type/class ChatGPTWebAdapter |
| 37 | method | `ID` | id |
| 38 | method | `Priority` | priority |
| 43 | method | `SupportsTools` | SupportsTools is false: ChatGPT web flattens to one text prompt and cannot emit Claude/OpenAI tool_use. Pool Send skips this adapter when the client sent tools… |
| 45 | method | `client` | client |
| 65 | func | `BuildConcatenatedPrompt` | BuildConcatenatedPrompt flattens a multi-turn ChatRequest into the single text blob the web adapters (ChatGPT, Claude web) send as one message. Multiple system… |
| 136 | func | `isChatGPTRateLimit` | is chat gpt rate limit |
| 148 | method | `SendMessageStream` | send message stream |
| 276 | method | `resetConversation` | reset conversation |
| 289 | method | `ResetConversation` | ResetConversation clears the server-side ChatGPT thread (provider handoff). |
| 291 | method | `persistConversation` | persist conversation |
| 305 | func | `streamChatGPTWeb` | stream chat gpt web |

### `pkg/provider/claude_web.go` (24 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | type | `ClaudeWebAdapter` | type/class ClaudeWebAdapter |
| 48 | method | `ID` | id |
| 49 | method | `Priority` | priority |
| 52 | method | `SupportsTools` | SupportsTools is false: claude.ai chat has no Anthropic tool_use wire. |
| 54 | method | `client` | client |
| 61 | method | `model` | model |
| 68 | method | `cookieHeader` | cookie header |
| 75 | func | `claudeCookiesHaveClearance` | claude cookies have clearance |
| 83 | method | `refreshCookiesFromProfile` | refreshCookiesFromProfile pulls Cloudflare cookies from the existing ~/.am/browser-profiles/claude jar. Bare sessionKey hits a bot rate bucket where turn 2 429… |
| 111 | method | `SendMessageStream` | send message stream |
| 227 | method | `ensureConversation` | ensureConversation reuses a persisted org+conversation across process restarts. Creates a new Claude thread only when none is cached (or after resetConversatio… |
| 250 | method | `resetConversation` | reset conversation |
| 258 | method | `ResetConversation` | ResetConversation clears the server-side Claude.ai thread (provider handoff). |
| 261 | method | `persistConversationLocked` | persistConversationLocked writes org/conv to accounts.json. Caller holds a.mu. |
| 267 | func | `lastUserPrompt` | last user prompt |
| 276 | method | `mapClaudeHTTPError` | map claude http error |
| 306 | func | `parseRetryAfterSeconds` | parse retry after seconds |
| 318 | func | `claudeFreeRateLimitErr` | claude free rate limit err |
| 330 | func | `setClaudeWebHeaders` | set claude web headers |
| 341 | method | `getOrganizationID` | get organization id |
| 392 | func | `DetectClaudeWebModel` | DetectClaudeWebModel queries claude.ai/api/organizations with a signed-in session and returns the best model that account's plan capabilities allow (Opus for p… |
| 433 | func | `pickClaudeWebModel` | pickClaudeWebModel maps an organization's capabilities (from claude.ai/api/organizations) to the best model that plan can use. |
| 443 | method | `createConversation` | create conversation |
| 503 | func | `streamClaudeWeb` | stream claude web |

### `pkg/provider/codex_cli.go` (28 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 29 | type | `CodexCLIAdapter` | CodexCLIAdapter reuses the OAuth token Codex CLI already stores at ~/.codex/auth.json after `codex login` (or this CLI's own `am add codex`, which snapshots th… |
| 47 | method | `ID` | id |
| 48 | method | `Priority` | priority |
| 49 | method | `Group` | group |
| 53 | method | `SupportsTools` | SupportsTools is true: Codex Responses API accepts native function tools. Claude / AGY client catalogs are converted via pkg/tools before POST. |
| 55 | method | `client` | client |
| 65 | func | `CodexAuthPath` | CodexAuthPath returns the path to Codex CLI's own credentials file — the same file `codex login` writes and refreshes, so reading/refreshing it here stays in… |
| 77 | func | `CodexAuthAvailable` | CodexAuthAvailable reports whether a parseable Codex CLI auth file with a live access token exists, without caring whether it's expired (that's handled/refresh… |
| 85 | func | `readCodexAuthDoc` | read codex auth doc |
| 97 | func | `codexAuthTokenField` | codex auth token field |
| 110 | func | `writeCodexAuthTokens` | writeCodexAuthTokens persists a refreshed access/refresh token pair back into the auth file, preserving every other field verbatim (round-tripped through map[s… |
| 132 | func | `decodeJWTExpSeconds` | decodeJWTExpSeconds pulls the "exp" claim (epoch seconds) out of a JWT without verifying its signature — same trust level as the rest of this adapter, which … |
| 154 | func | `refreshCodexToken` | refresh codex token |
| 189 | method | `SendMessageStream` | send message stream |
| 325 | func | `isCodexCompatibleModel` | is codex compatible model |
| 341 | func | `streamCodexResponses` | streamCodexResponses parses the Responses-API SSE event shape (response.output_text.delta / function_call items / response.completed). |
| 418 | type | `codexResponsesEvent` | type/class codexResponsesEvent |
| 431 | type | `codexResponsesItem` | type/class codexResponsesItem |
| 440 | type | `codexCallAcc` | type/class codexCallAcc |
| 445 | func | `newCodexCallAcc` | create codex call acc |
| 449 | method | `key` | key |
| 456 | method | `get` | get |
| 476 | method | `observeItem` | observe item |
| 489 | method | `observeOutput` | observe output |
| 495 | method | `appendArgs` | append args |
| 506 | method | `setArgs` | set args |
| 519 | method | `observeName` | observe name |
| 526 | method | `flush` | flush |

### `pkg/provider/config.go` (49 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | type | `DuplicateAPIKeyError` | DuplicateAPIKeyError means the same credential (API key / session token / session key) is already bound to another provider — one secret → one entry. |
| 27 | method | `Error` | error |
| 31 | type | `ProviderConfig` | type/class ProviderConfig |
| 79 | method | `IsConfigured` | IsConfigured reports whether the provider has valid credentials and is in the rotate pool (Enabled != false). |
| 87 | method | `InRotatePool` | InRotatePool is true unless Enabled is explicitly false. |
| 92 | method | `HasCredentials` | HasCredentials reports whether secrets/config look usable (ignores Enabled). |
| 111 | func | `InferIDE` | InferIDE maps a stored provider onto the IDE that owns it. |
| 134 | method | `Normalize` | Normalize fills ide so older accounts.json rows match the current schema. |
| 143 | func | `NormalizeAccountsFile` | normalize accounts file |
| 155 | type | `AccountsFile` | type/class AccountsFile |
| 161 | func | `DefaultAccountsPath` | default accounts path |
| 200 | func | `PoolIDPrefix` | PoolIDPrefix returns the unified-ID prefix for a built-in pool provider type (e.g. "claude_web" -> "claude:web"), or "" if the type has no fixed prefix (openai… |
| 206 | func | `NextIDForPrefix` | NextIDForPrefix returns FormatID(prefix, maxN+1) based on existing providers whose ParseID prefix matches. Used when adding another OpenRouter (etc.) key. |
| 229 | func | `hostMatches` | hostMatches parses rawURL and checks whether its hostname exactly matches or is a subdomain of any of the given domains (e.g. "api.x.ai" matches "x.ai"). |
| 252 | func | `IsOpenRouterEndpoint` | IsOpenRouterEndpoint reports whether baseURL points at OpenRouter. |
| 257 | func | `IsKimiEndpoint` | IsKimiEndpoint reports whether baseURL points at Moonshot / Kimi. |
| 262 | func | `IsGrokEndpoint` | IsGrokEndpoint reports whether baseURL points at xAI / Grok. |
| 273 | func | `MigrateLegacyIDs` | MigrateLegacyIDs rewrites any provider in accounts.json still using one of the old fixed literal IDs (claude-web, chatgpt-web, google-ai-studio, github-models,… |
| 332 | func | `remapAccountLogs` | remap account logs |
| 341 | func | `remapJSONLAccountFields` | remapJSONLAccountFields rewrites "account" values (and free-text "msg") in a JSONL file from compact IDs (geminiapi:01) to brand:method form. |
| 398 | func | `SetPriority` | SetPriority updates the priority of one provider by ID and persists it. If id isn't found and looks like a "codex_cli"-shaped unified ID (the Codex CLI token-r… |
| 425 | func | `SetEnabled` | SetEnabled turns a pool provider on or off. Disabled (Enabled:false) means removed from rotate/failover pool; the account stays in accounts.json and can still … |
| 452 | func | `SetModel` | SetModel updates the target model of one provider by ID and persists it. |
| 471 | func | `ResolveSecret` | resolve secret |
| 478 | func | `BuildAdapter` | build adapter |
| 592 | func | `LoadAccounts` | LoadAccounts reads accounts from accounts.json, returning only rotate-pool adapters (Enabled != false + credentials). |
| 601 | func | `LoadAllAddressable` | LoadAllAddressable returns every adapter with credentials that is not turned off (Enabled != false) — including ones outside the priority rotate order, so X-… |
| 607 | func | `LookupAdapter` | LookupAdapter builds one adapter by ID, even when out of the priority rotate order — but never for a disabled ("am off") provider. |
| 624 | func | `loadAccounts` | load accounts |
| 739 | func | `codexPoolAdapter` | codexPoolAdapter builds the Codex CLI token-reuse adapter if a live ~/.codex/auth.json is present. providers is accounts.json's current provider list, consulte… |
| 771 | func | `CodexAutoRow` | CodexAutoRow returns a synthetic display row for the auto-surfaced Codex CLI token-reuse adapter, for callers like `am accounts` that want to show it even thou… |
| 783 | func | `codexPoolID` | codexPoolID resolves the unified ID of the currently active codex profile, falling back to the first saved codex profile, then to "codex:01" if no profile has … |
| 798 | func | `agyPoolAdapter` | agy pool adapter |
| 837 | func | `AGYAutoRow` | AGYAutoRow returns a synthetic display row for auto-surfaced Antigravity adapter. |
| 845 | func | `agyPoolID` | agy pool id |
| 860 | func | `LoadConfigFile` | load config file |
| 873 | func | `SaveConfigFile` | save config file |
| 895 | func | `readAccountsFileBytes` | readAccountsFileBytes returns the plaintext JSON for path, transparently decrypting if the file was written by writeAccountsFileBytes. A file that predates thi… |
| 917 | func | `writeAccountsFileBytes` | writeAccountsFileBytes encrypts plain (a full accounts.json document) with the machine's master key before writing it to path. |
| 928 | func | `sameSecret` | sameSecret reports whether two stored credential fields refer to the same secret (resolved env: refs, or identical raw strings including env:FOO). |
| 940 | func | `providerSecrets` | provider secrets |
| 952 | func | `findDuplicateCredentialID` | findDuplicateCredentialID returns another provider's ID that already owns any of p's secrets (api key / session token / session key). |
| 983 | func | `DeduplicateProvidersByCredential` | DeduplicateProvidersByCredential drops later entries that reuse an earlier provider's API key / session token / session key. Keeps first occurrence. |
| 1016 | func | `AddOrUpdateProvider` | add or update provider |
| 1079 | func | `UpdateProviderCookies` | UpdateProviderCookies merges a full Cookie header into an existing provider entry (used after CDP refresh so Cloudflare clearance stays current). |
| 1099 | func | `UpdateProviderConversation` | UpdateProviderConversation persists Claude web org+conversation IDs so the next process reuses the same thread (empty conv clears → next send creates). |
| 1108 | type | `ChatState` | ChatState is the persisted multi-turn thread for web providers. |
| 1119 | func | `UpdateProviderChatState` | UpdateProviderChatState merges conversation continuity fields for web adapters. |
| 1145 | func | `RemoveProvider` | remove provider |

### `pkg/provider/gemini.go` (8 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 28 | type | `GeminiAdapter` | GeminiAdapter wraps Google AI Studio's OpenAI-compatible endpoint. |
| 40 | func | `NewGeminiAdapter` | create gemini adapter |
| 68 | method | `ID` | id |
| 69 | method | `Priority` | priority |
| 71 | method | `SendMessageStream` | send message stream |
| 122 | type | `geminiModelsResponse` | geminiModelsResponse is the subset of v1beta/models we need to pick a default: name ("models/gemini-3.6-pro") and which generation methods the key is actually … |
| 135 | func | `DetectGeminiModel` | DetectGeminiModel queries Google AI Studio's models endpoint with apiKey and returns the best model that key can actually call via generateContent, preferring … |
| 168 | func | `pickGeminiModel` | pickGeminiModel chooses the best callable gemini-* model from a decoded v1beta/models response: pro > flash (non-lite) > whatever else is offered. |

### `pkg/provider/gemini_web.go` (20 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 30 | type | `GeminiWebAdapter` | GeminiWebAdapter talks to gemini.google.com StreamGenerate using browser session cookies (__Secure-1PSID + jar), not an AI Studio API key. |
| 59 | method | `ID` | id |
| 60 | method | `Priority` | priority |
| 63 | method | `SupportsTools` | SupportsTools is false: Gemini web StreamGenerate is text-only. |
| 65 | method | `client` | client |
| 72 | method | `SendMessageStream` | send message stream |
| 115 | method | `ensureInit` | ensure init |
| 160 | method | `loadMetadata` | load metadata |
| 173 | method | `persistMetadata` | persist metadata |
| 190 | method | `resetConversation` | reset conversation |
| 202 | method | `ResetConversation` | ResetConversation clears the server-side Gemini thread (provider handoff). |
| 204 | method | `streamGenerate` | stream generate |
| 318 | func | `buildGeminiStreamInner` | build gemini stream inner |
| 356 | func | `geminiEntropyToken` | gemini entropy token |
| 362 | func | `geminiHexUUID` | gemini hex uuid |
| 368 | func | `geminiEnvelopeError` | gemini envelope error |
| 389 | func | `geminiDrillInt` | gemini drill int |
| 410 | func | `geminiParseEnvelope` | gemini parse envelope |
| 479 | func | `isGeminiUsageLimit` | is gemini usage limit |
| 492 | func | `isGeminiAuthErr` | is gemini auth err |

### `pkg/provider/http_client.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 28 | func | `newDefaultTransport` | create default transport |

### `pkg/provider/match.go` (2 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 11 | func | `MatchID` | MatchID resolves a pool provider id. Accepts a full id (`gemini:web:01`) or a unique prefix (`gemini:web`, `chatgpt`). Ambiguous prefixes error. |
| 43 | func | `listedProviderIDs` | listed provider i ds |

### `pkg/provider/openai.go` (10 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | type | `OpenAICompatibleAdapter` | OpenAICompatibleAdapter wraps any endpoint speaking the OpenAI /v1/chat/completions wire protocol (GitHub Models, Google AI Studio, Groq, DeepSeek, vLLM, Ollam… |
| 31 | method | `ID` | id |
| 32 | method | `Priority` | priority |
| 33 | method | `Group` | group |
| 37 | method | `SupportsTools` | SupportsTools is true: OpenAI-compatible upstreams (including AGY) speak native function tools. Claude / Codex catalogs are converted in pkg/tools. |
| 39 | method | `client` | client |
| 51 | method | `SendMessageStream` | SendMessageStream posts req (with Model swapped for TargetModel) to BaseURL+"/chat/completions" and streams the SSE reply back as types.StreamChunk values. Too… |
| 114 | func | `streamOpenAISSE` | stream open aisse |
| 118 | type | `deltaToolCall` | type/class deltaToolCall |
| 128 | type | `accCall` | type/class accCall |

### `pkg/provider/pool_slot.go` (8 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | func | `NamedPoolID` | NamedPoolID returns the short identity ID for a provider type + email (e.g. claude_web + ninhle@x.com → "claude:web:ninhle"; chatgpt_web → "chatgpt:ninhle"… |
| 35 | func | `NamedPoolIDWithDomain` | NamedPoolIDWithDomain returns the disambiguated identity ID when two emails share a local part: claude_web + ninhle@gmail.com → "claude:web:ninhle-gmailcom". |
| 47 | type | `PoolSlot` | PoolSlot is the result of resolving where a login should land in the pool. |
| 60 | func | `ResolvePoolSlot` | ResolvePoolSlot finds an existing pool entry for the same account identity, or allocates a new named/numeric ID. email may be empty (falls back to next numeric… |
| 159 | func | `chooseNamedPoolID` | chooseNamedPoolID picks short local-part ID, or local-domain when another pool account already uses the same local part under a different email. |
| 197 | func | `idTaken` | id taken |
| 209 | func | `defaultPriorityForType` | default priority for type |
| 226 | func | `UpsertPoolProvider` | UpsertPoolProvider writes p into the pool. If renameFrom is set, the existing row with that ID is replaced (ID change) instead of appending. |

### `pkg/provider/prompt.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | func | `PromptWithSystem` | PromptWithSystem prepends all system turns to userPrompt. Web adapters that only POST a single completion string (Claude/ChatGPT/Gemini web) must use this so C… |
| 38 | func | `WebBackendPrompt` | WebBackendPrompt builds the single string web UIs accept. FullContext (Claude Code / Codex via proxy): flatten entire history and skip server-side thread conti… |
| 75 | func | `slimWebMessages` | slimWebMessages drops client harness system turns. User/tool/assistant stay. |
| 100 | func | `stripWebUserNoise` | strip web user noise |
| 108 | func | `isClientHarness` | is client harness |

### `pkg/provider/stream.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 9 | func | `sendChunk` | send chunk |

### `pkg/provider/uuid.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 11 | func | `newUUIDv4` | newUUIDv4 returns a random UUID v4 string. Used by web adapters whose upstream APIs require UUID message/conversation ids (empty or "msg-N" forms are rejected … |

## `pkg/proxy` — 11 files · 123 funcs

Keywords: proxy, pkg, gateway, http

### `pkg/proxy/addr.go` (12 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | func | `ComposeListenAddr` | ComposeListenAddr builds host:port for the daemon. public forces host 0.0.0.0. Empty host defaults to 127.0.0.1; empty port to 8787. If host already contains a… |
| 55 | func | `ParseListenArgs` | ParseListenArgs reads --public, --addr/-b, --port/-p from argv. ok is false when none of those flags appear (caller keeps env/persisted/default). |
| 106 | func | `ListenAddrPath` | ListenAddrPath is deprecated legacy path; persistence lives in proxy.bind.json. Kept for tests that historically wrote proxy-listen. |
| 112 | func | `SaveListenAddr` | SaveListenAddr remembers where the daemon should bind by updating proxy.bind.json (public + port). Also mirrors to legacy proxy-listen. |
| 126 | func | `LoadListenAddr` | LoadListenAddr returns the persisted bind address from bind config, or "". |
| 141 | func | `ResolveListenAddr` | ResolveListenAddr picks the bind address for the proxy daemon. Order: explicit override → AM_PROXY_LISTEN → bind config / legacy → AM_PROXY_ADDR → loca… |
| 164 | func | `normalizeListenAddr` | normalizeListenAddr accepts host, host:port, or :port. |
| 181 | func | `DialAddr` | DialAddr is where local `am` talks to the proxy. Binding 0.0.0.0 is not a connectable host, so we rewrite it (and ::) to 127.0.0.1 for loopback. |
| 196 | func | `IsPublicListen` | IsPublicListen reports whether the bind address accepts remote clients. |
| 205 | func | `PrimaryLANIP` | PrimaryLANIP returns a best-effort non-loopback IPv4 for status / am env. |
| 221 | func | `firstIPv4` | first i pv4 |
| 246 | func | `PublicBaseURL` | PublicBaseURL builds http://<lan-ip>:<port> when listening publicly. |

### `pkg/proxy/authtoken.go` (13 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | func | `authTokenPath` | authTokenPath is where the admin bearer token is persisted, 0600, generated when the proxy binds publicly. Loopback callers never need it (see requireAuth). |
| 30 | func | `IssueNewAuthToken` | IssueNewAuthToken creates a fresh API key with format amux-<token>, persists it to disk (0600) overwriting any previous key, and returns it. Each time a public… |
| 47 | func | `LoadAuthToken` | LoadAuthToken reads the currently persisted token from disk. |
| 61 | func | `ClearAuthToken` | ClearAuthToken removes the persisted token file when the public proxy is stopped. |
| 72 | func | `LoadOrCreateAuthToken` | LoadOrCreateAuthToken returns the persisted amux admin token, generating a new one with amux- prefix if none exists or if the existing one is invalid. |
| 80 | func | `isLoopback` | isLoopback reports whether r arrived over a loopback connection. |
| 90 | func | `requestToken` | requestToken extracts the bearer token from X-Am-Token, X-Api-Key, x-goog-api-key, api-key, Authorization, or ?key=. |
| 140 | func | `StopAuthRateLimiter` | StopAuthRateLimiter signals the background eviction goroutine to exit cleanly. Call this during graceful server shutdown to avoid goroutine leaks. Safe to call… |
| 144 | func | `init` | initialize |
| 174 | func | `isAuthRateLimited` | is auth rate limited |
| 194 | func | `recordFailedAuth` | record failed auth |
| 237 | func | `recordSuccessfulAuth` | record successful auth |
| 251 | func | `requireAuth` | requireAuth wraps h so non-loopback requests must present the valid amux-<auth-token> when the daemon is bound publicly (0.0.0.0/:: or external IP). Loopback r… |

### `pkg/proxy/bind.go` (16 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 14 | type | `bindConfig` | type/class bindConfig |
| 19 | func | `bindConfigPath` | bind config path |
| 23 | func | `loadBindConfig` | load bind config |
| 35 | func | `saveBindConfig` | save bind config |
| 45 | func | `SaveBindPublic` | SaveBindPublic persists whether the daemon should listen on 0.0.0.0. |
| 55 | func | `SaveBindPort` | SaveBindPort persists the daemon listen port (keeps public flag). |
| 67 | func | `SaveBindListen` | SaveBindListen derives public/port from a full listen addr (host:port) and persists them in proxy.bind.json — single source of truth for ListenAddr(). |
| 90 | func | `IsPublic` | IsPublic reports saved public-bind preference. |
| 93 | func | `listenPort` | listenPort extracts the port from AM_PROXY_ADDR / default client addr. |
| 111 | func | `ProxyAddr` | ProxyAddr is the address local clients use for health checks / base URL (always loopback — even when the daemon binds 0.0.0.0). |
| 118 | func | `proxyAddr` | proxy addr |
| 121 | func | `ListenAddr` | ListenAddr is the bind address for the daemon process. |
| 130 | func | `IsPublicBind` | IsPublicBind reports whether a listen addr is public (0.0.0.0 / ::). |
| 140 | func | `LocalIPv4s` | LocalIPv4s returns non-loopback IPv4 addresses for public URL display. |
| 182 | func | `PublicURLs` | PublicURLs returns http://<lan-ip>:<port> for each local IPv4. |
| 193 | func | `FormatPublicHosts` | FormatPublicHosts joins public URLs for status display. |

### `pkg/proxy/btw.go` (8 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 17 | type | `BtwQueue` | BtwQueue is a thread-safe, size-bounded queue of "by-the-way" messages injected into the next outgoing LLM request while an agent is running. Users send messag… |
| 22 | type | `btwEntry` | type/class btwEntry |
| 36 | func | `GetGlobalBtwQueue` | GetGlobalBtwQueue returns the singleton BtwQueue used by the proxy. |
| 39 | method | `Push` | Push adds a message to the queue (bounded, thread-safe). |
| 55 | method | `Drain` | Drain returns all pending messages and empties the queue atomically. Returns nil when empty. |
| 70 | method | `Len` | Len returns current queue depth (for status reporting). |
| 79 | func | `HandleBtw` | HandleBtw is the HTTP handler for POST /_am/btw. Body: JSON {"text":"..."} OR raw text in query param ?text=... Also handles GET /_am/btw for status. |
| 132 | func | `BuildBtwInjection` | BuildBtwInjection formats pending BTW messages as a system note to prepend to the user prompt. Returns "" when there are no pending messages. |

### `pkg/proxy/client.go` (17 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 20 | func | `ProxyBase` | proxy base |
| 24 | func | `ProxyUp` | proxy up |
| 34 | func | `resolveAMBin` | resolve am bin |
| 44 | func | `proxyStatusMode` | proxy status mode |
| 60 | type | `UpFlags` | UpFlags controls `amux proxy up` / `amux proxy --public` behaviour. |
| 68 | func | `CmdProxyUp` | cmd proxy up |
| 78 | func | `CmdProxyUpWithAddr` | CmdProxyUpWithAddr starts (or attaches to) the proxy. listenOverride comes from `am proxy up --public` / `--addr` / `--port`; empty keeps saved bind. |
| 86 | func | `CmdProxyUpFlags` | cmd proxy up flags |
| 186 | func | `Sync` | sync |
| 192 | func | `postAndClose` | post and close |
| 200 | func | `RegisterSession` | RegisterSession registers (or deregisters) a client process with the supervisor. |
| 206 | func | `attachedSessions` | attached sessions |
| 221 | func | `CmdProxyDown` | cmd proxy down |
| 265 | func | `CmdSwitch` | cmd switch |
| 292 | func | `CmdSwitchProvider` | cmd switch provider |
| 313 | func | `CmdBtw` | CmdBtw sends a "by-the-way" message to be injected into the next LLM request while an agent is running. Usage: am btw <message text> |
| 348 | func | `jsonQuote` | jsonQuote returns a JSON-encoded double-quoted string for simple text. |

### `pkg/proxy/lifecycle.go` (6 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 11 | type | `Lifecycle` | Lifecycle tracks active Claude sessions by PID for status reporting. |
| 16 | func | `NewLifecycle` | create lifecycle |
| 20 | method | `AddSession` | add session |
| 33 | method | `EndSession` | end session |
| 39 | method | `Sessions` | sessions |
| 43 | method | `pruneDead` | prune dead |

### `pkg/proxy/passthrough.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 32 | func | `newPassthroughHandler` | newPassthroughHandler serves a minimal subset of the full proxy: every non-admin request is forwarded straight to the real Anthropic API via the same reverse-p… |

### `pkg/proxy/rotator.go` (28 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 35 | func | `SetUsedThreshold` | SetUsedThreshold records the auto-rotate utilization threshold for subsequent NewRotator calls. Accepts either a percent (95) or a fraction (0.95); invalid / z… |
| 40 | func | `UsedThreshold` | UsedThreshold returns the currently configured package default. |
| 44 | func | `ParseUsedThreshold` | ParseUsedThreshold normalizes a CLI / env value to a 0–1 fraction. Values > 1 are treated as percents (95 → 0.95). ≤0 or >100 → default. |
| 58 | type | `Rotator` | Rotator holds the in-memory token state and auto-rotates on rate limits. |
| 84 | func | `NewRotator` | create rotator |
| 90 | method | `Load` | load |
| 126 | method | `RefreshFromDisk` | RefreshFromDisk picks up any profile saved since Load() (a fresh `am add`, or a new login) without disturbing in-memory rotation state (idx, cooldown, switch c… |
| 200 | method | `Names` | names |
| 208 | method | `Active` | active |
| 217 | method | `SetActive` | set active |
| 235 | method | `Token` | Token returns the current access token for the active account. The active account's credential lives in the system keychain and Claude Code is the one that ref… |
| 308 | method | `Observe` | Observe reads rate-limit headers off each response and rotates if needed. A 401 marks the active account's refresh dead and rotates immediately — that's Clau… |
| 393 | method | `AllUnavailable` | AllUnavailable reports whether every saved profile is either in cooldown, marked dead, or turned off — i.e. Claude reverse-proxy has nowhere useful to go and… |
| 417 | method | `ProfileCount` | ProfileCount returns how many Claude Code profiles are usable — i.e. not turned off (`am off`). A disabled profile must never be treated as an available opti… |
| 431 | method | `TotalProfileCount` | TotalProfileCount returns every Claude Code profile the rotator knows about, including disabled ones — for display/status purposes only. |
| 439 | method | `ShouldFailoverToProviderPool` | ShouldFailoverToProviderPool is true when every Claude Code profile is cooling, dead, or turned off. Callers then bridge to the provider pool. |
| 447 | method | `EnsureUsableActive` | EnsureUsableActive makes sure the active Claude profile is one that is not cooling/dead. Used when returning from provider-pool failover after a rate-limit win… |
| 500 | func | `parseWindow` | parseWindow reads anthropic-ratelimit-unified-<suffix>-{utilization,reset} for one window ("5h" or "7d"). Known=false when the header wasn't sent. |
| 520 | method | `PeriodicSnapshot` | PeriodicSnapshot keeps the active profile's bundle in sync with whatever Claude Code has rotated into the live keychain. Refresh tokens are single-use/rotate-o… |
| 528 | method | `snapshotActiveIfChanged` | snapshot active if changed |
| 552 | method | `Rotate` | rotate |
| 600 | method | `ForceSwitch` | ForceSwitch makes the named profile active immediately (the next request uses it). Used by `am switch` / the `/_am/switch` admin endpoint — unlike Rotate, th… |
| 607 | method | `ForceSwitchExplicit` | ForceSwitchExplicit selects a profile for API X-Provider routing. Off profiles are still rejected — X-Provider is a routing hint, not a way to bypass `am off… |
| 611 | method | `forceSwitch` | force switch |
| 660 | method | `EvictDisabledActive` | EvictDisabledActive switches away from the active profile if it was just turned off (`am off`). No-op when active is still enabled. |
| 678 | method | `Status` | status |
| 730 | func | `parseFirstFloat` | parse first float |
| 741 | func | `parseFirstTime` | parse first time |

### `pkg/proxy/server.go` (18 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 32 | type | `ProxyMode` | type/class ProxyMode |
| 37 | method | `Get` | get |
| 46 | method | `Set` | set |
| 56 | type | `swappableHandler` | swappableHandler lets /_am/shutdown swap in a stripped-down passthrough handler (see passthrough.go) for a brief grace window before the socket actually closes… |
| 61 | method | `Set` | set |
| 67 | method | `ServeHTTP` | serve http |
| 83 | func | `RunProxy` | RunProxy starts the server on the given address, serving Claude Code, OpenAI gateway, and administrative endpoints. |
| 155 | func | `hasCallerCredential` | hasCallerCredential reports whether the incoming request already carries its own Anthropic credential (an API key, or an OAuth-style bearer token from the clie… |
| 198 | func | `newReverseProxy` | newReverseProxy builds the httputil.ReverseProxy that forwards to the real Anthropic API (or whatever `upstream` points at), injecting either the rotator's OAu… |
| 251 | type | `dynamicProxyRoundTripper` | type/class dynamicProxyRoundTripper |
| 255 | method | `RoundTrip` | round trip |
| 286 | func | `newHandler` | newHandler builds the full HTTP handler serving Claude Code, the OpenAI gateway, and the /_am/ admin endpoints. chatPool serves /v1/chat/completions; toolPool … |
| 648 | func | `isAnthropicClient` | isAnthropicClient reports whether the caller is speaking Anthropic's API (Claude Code, Anthropic SDK) rather than OpenAI's. Same path (/v1/models) must not ret… |
| 666 | func | `shouldObserveUpstream` | shouldObserveUpstream is true for quota-bearing Anthropic hops (POST /v1/messages). GET catalog and count_tokens 429/401 must not rotate or quarantine the Clau… |
| 683 | func | `anthropicUpstreamReady` | anthropicUpstreamReady is true when this proxy can authenticate an Anthropic reverse-proxy hop: caller brought a real key, or the Claude rotator has a usable O… |
| 700 | func | `handleAmMapBundle` | handle am map bundle |
| 720 | func | `anthropicRequestHasTools` | anthropicRequestHasTools reports whether a /v1/messages body includes a non-empty tools array (Claude Code agent requests). |
| 732 | func | `redactOutboundBody` | redactOutboundBody rewrites r.Body in place, replacing secrets with samples before httputil.ReverseProxy dials the upstream. Safe to call repeatedly. |

### `pkg/proxy/supervisor.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 27 | func | `RunSupervisor` | RunSupervisor is what `am proxy --supervise` runs (spawned by CmdProxyUp instead of the bare server directly). It keeps a real `am proxy` server child alive: -… |
| 110 | func | `superBackoff` | superBackoff returns the delay before the (attempt+1)th respawn: 0.5s, 1s, 2s, 4s, 8s, 8s, ... — capped at supervisorMaxBackoff. |
| 123 | func | `crashLooping` | crashLooping reports whether history contains supervisorMaxCrashes or more entries within supervisorCrashWindow of now. |

### `pkg/proxy/tunnel.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | func | `handleConnectTunnel` | handleConnectTunnel implements standard HTTP CONNECT tunneling so clients that use HTTP_PROXY / HTTPS_PROXY (e.g. agy, curl, git, SDKs) can use amux as a forwa… |

## `pkg/router` — 5 files · 53 funcs

Keywords: router, pkg

### `pkg/router/classifier.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 23 | type | `TaskClassification` | TaskClassification holds the analysis result of a ChatRequest. |
| 98 | func | `ClassifyTask` | ClassifyTask evaluates an incoming ChatRequest for pro/thinking escalation and a soft task Kind used only for account-group preference. |
| 208 | func | `detectTaskKind` | detect task kind |
| 239 | func | `requestHasMutatingTools` | request has mutating tools |

### `pkg/router/group.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 56 | func | `IDEFromClientDialect` | IDEFromClientDialect maps a ChatRequest.ClientDialect (claude/codex/gemini/cursor) onto the IDE whose subscription accounts should be main. |
| 72 | func | `NativeGroups` | NativeGroups are the main (same-IDE) groups for an inbound client. |
| 87 | func | `GroupPriorityForIDE` | GroupPriorityForIDE puts this IDE's subscription/free groups first, then every other group in the default order (those are proxy/failover). |
| 108 | func | `GroupDisplayName` | GroupDisplayName returns a clean human-readable name for each group. |
| 136 | type | `GroupAwareAdapter` | GroupAwareAdapter is optionally implemented by adapters that have explicit group info. |
| 141 | func | `GroupIndex` | GroupIndex returns the GroupPriority rank (0 = highest). Unknown groups sort last. |
| 152 | func | `ResolveAccountGroup` | ResolveAccountGroup maps a stored account onto a rotate group. explicit wins; otherwise type / id / plan heuristics match DetermineAdapterGroup. |
| 189 | func | `ResolveProfileGroup` | ResolveProfileGroup maps a CLI profile (claude / codex / antigravity) onto a group. |
| 204 | func | `DetermineAdapterGroup` | DetermineAdapterGroup resolves the group for any ProviderAdapter. |

### `pkg/router/pool.go` (26 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | func | `redactBeforeSend` | redactBeforeSend is the universal outbound gate: every adapter path (proxy bridge, gateway, tests) must pass here before network I/O. |
| 50 | type | `AccountPoolRouter` | AccountPoolRouter dispatches a ChatRequest to the highest-priority adapter that isn't currently cooling down. A preferred adapter from `am sw <provider>` is tr… |
| 64 | func | `NewAccountPoolRouter` | NewAccountPoolRouter builds a router over adapters, sorted once by Priority() ascending (1 tried first). |
| 82 | method | `SetDirectory` | SetDirectory replaces the explicit-routing map (may include adapters not in the rotate pool). Rotate order is unchanged. |
| 99 | func | `applyTaskClassification` | applyTaskClassification inspects the request and dynamically enables thinking mode or escalates to Pro tier when heavy analytical reasoning is required. Also s… |
| 125 | method | `SendNamed` | SendNamed routes to one adapter by ID (rotate pool or out-of-pool directory). |
| 156 | method | `SetPreferred` | SetPreferred sets a MANUAL pin (`am sw <provider>`). New sessions stick to this adapter until cleared or failed over. Auto-failover leftovers must not call thi… |
| 164 | method | `ClearPreferred` | ClearPreferred drops the manual pin so session load-balance resumes. |
| 172 | method | `ManualPin` | ManualPin reports whether preferred came from `am sw` (not auto-switch). |
| 181 | type | `ConversationResetter` | ConversationResetter is implemented by web adapters that keep a server-side chat thread. Called on `am sw` so the next turn does not continue an unrelated conv… |
| 188 | method | `ResetConversations` | ResetConversations clears server-side web threads on every adapter that supports it (Claude/ChatGPT/Gemini web). Safe to call when switching providers so Claud… |
| 200 | method | `Preferred` | Preferred returns the currently preferred adapter ID. |
| 207 | method | `LastUsed` | LastUsed returns the adapter ID that last successfully started a stream. |
| 215 | method | `Len` | Len returns how many adapters the pool currently holds (including auto-surfaced Codex fallback). |
| 221 | type | `toolCapability` | type/class toolCapability |
| 225 | func | `adapterSupportsTools` | adapter supports tools |
| 236 | func | `skipTextOnly` | skip text only |
| 240 | method | `usableToolBackend` | usable tool backend |
| 270 | method | `Send` | Send tries adapters in priority order. Routing precedence: 1. Session affinity pin (same tool-loop stays on one account) 2. Manual `am sw` preferred pin 3. New… |
| 451 | method | `adapterAlive` | adapter alive |
| 472 | method | `pickSessionAdapter` | pickSessionAdapter returns the next living proxy-layer adapter for a new session (round-robin). Nil when the pool is empty / all cooling. |
| 484 | method | `cooling` | cooling |
| 497 | method | `setCooldown` | set cooldown |
| 506 | method | `markUsed` | markUsed records lastUsed; when promote is true also updates preferred for status display and CLEARS manualPin so auto leftovers do not steal the next new sess… |
| 517 | method | `Status` | Status returns a summary map of each adapter, its priority, cooldown, and whether it is preferred. |
| 555 | method | `Reload` | Reload updates the router's rotate-pool adapters and merges them into the addressable directory (clearing any stale or disabled adapters). |

### `pkg/router/session_lb.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 36 | func | `RoleForGroup` | RoleForGroup returns the coding-layer role for a pool group. |
| 56 | func | `RoleForAdapter` | RoleForAdapter resolves the role for a live adapter. |
| 62 | func | `IsClaudeSubscriptionGroup` | IsClaudeSubscriptionGroup is true for Claude OAuth / free subscription pool groups. Session balancer never picks these as proxy backends. |
| 69 | func | `ProxyGroupsForClient` | ProxyGroupsForClient returns group try-order for the provider pool when the client is Claude IDE: skip Claude subscription (rotator-only), put proxy layers fir… |
| 91 | func | `balanceRank` | balance rank |
| 102 | func | `sortForSessionBalance` | sortForSessionBalance orders adapters by SessionBalanceOrder, preserving relative order within the same group. |
| 116 | func | `isPrimaryProxyLayer` | is primary proxy layer |
| 130 | method | `livingForSessionBalance` | livingForSessionBalance returns adapters eligible for a new session assignment: not Claude-sub, not cooling/quarantined, and usable for the request's tool need… |
| 156 | func | `AdapterIDPrefix` | AdapterIDPrefix matches common pool id shapes for role lookup by string. |

### `pkg/router/task_prefer.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 12 | func | `PreferredGroupsForTask` | PreferredGroupsForTask returns soft-preference group order for a task kind. These groups are tried first; every other eligible group remains available. Empty k… |
| 58 | func | `SoftPreferGroups` | SoftPreferGroups reorders base so prefer appears first (intersection only), then the remaining base groups. Never drops a group from base. |
| 84 | func | `GroupOrderForRequest` | GroupOrderForRequest is the try-order for pool failover: IDE proxy rules, then soft task preference. Manual pin / affinity still win before this list. |
| 95 | func | `groupRankIn` | group rank in |
| 105 | func | `sortAdaptersByGroupOrder` | sortAdaptersByGroupOrder stable-sorts adapters by group rank in order. |

## `pkg/term` — 2 files · 64 funcs

Keywords: term, pkg

### `pkg/term/log.go` (14 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 25 | func | `tagPaint` | tag paint |
| 48 | func | `stamp` | stamp |
| 59 | func | `Log` | Log writes a tagged realtime line to stderr and persists it to events.log. 15:04:05 amux [ROTATE ] ninhle → tungnt (rate-limit) |
| 78 | func | `SetEventSink` | SetEventSink registers a callback for Log persistence (used by monitor). |
| 86 | func | `LogProxy` | log proxy |
| 87 | func | `LogRotate` | log rotate |
| 88 | func | `LogSwitch` | log switch |
| 89 | func | `LogFailover` | log failover |
| 90 | func | `LogAuth` | log auth |
| 91 | func | `LogPool` | log pool |
| 92 | func | `LogOK` | log ok |
| 93 | func | `LogWarn` | log warn |
| 94 | func | `LogErr` | log err |
| 95 | func | `LogDegraded` | log degraded |

### `pkg/term/style.go` (50 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 47 | func | `init` | initialize |
| 52 | func | `detectDark` | detectDark: AMUX_THEME=dark/light overrides; else COLORFGBG; default dark. |
| 80 | func | `applyTheme` | apply theme |
| 101 | func | `SetTheme` | SetTheme forces "dark" or "light" (tests / AMUX_THEME). |
| 108 | func | `Disable` | Disable turns colors off (tests / piped output). |
| 111 | func | `Enable` | Enable re-enables auto detection. |
| 114 | func | `SetQuiet` | SetQuiet suppresses tagged Log lines on stderr (chat UI). Events still persist. |
| 120 | func | `colorEnabled` | color enabled |
| 136 | func | `outOK` | out ok |
| 137 | func | `errOK` | err ok |
| 139 | func | `paint` | paint |
| 147 | func | `Bold` | Bold / Dim / Cyan / … paint for stdout. |
| 148 | func | `Dim` | dim |
| 149 | func | `Cyan` | cyan |
| 150 | func | `Green` | green |
| 151 | func | `Yellow` | yellow |
| 152 | func | `Red` | red |
| 153 | func | `Magenta` | magenta |
| 154 | func | `Blue` | blue |
| 155 | func | `White` | white |
| 156 | func | `Gray` | gray |
| 159 | func | `BoldErr` | BoldErr paints for stderr (proxy logs). |
| 160 | func | `CyanErr` | cyan err |
| 161 | func | `GreenErr` | green err |
| 162 | func | `YellowErr` | yellow err |
| 163 | func | `RedErr` | red err |
| 164 | func | `DimErr` | dim err |
| 165 | func | `MagentaErr` | magenta err |
| 168 | func | `DotLive` | Dot serving / idle / off. |
| 174 | func | `DotIdle` | dot idle |
| 180 | func | `DotWarn` | dot warn |
| 186 | func | `DotDead` | dot dead |
| 194 | func | `Badge` | Badge renders a status chip. Short labels (<=4) are padded so columns align. |
| 222 | func | `ProgressBar` | ProgressBar renders a filled bar for pct in [0,1]. Uses ASCII #/. so every terminal font renders correctly. |
| 257 | func | `VisibleLen` | VisibleLen counts printable runes, ignoring ANSI escapes. |
| 260 | func | `visibleLen` | visibleLen counts runes ignoring ANSI CSI sequences. |
| 279 | func | `padVisible` | pad visible |
| 287 | func | `truncateVisible` | truncate visible |
| 318 | func | `Section` | Section prints a clean, bold uppercase section label. |
| 325 | func | `PanelEnd` | PanelEnd closes the section cleanly. |
| 328 | func | `Header` | Header prints a modern, minimalist header banner. |
| 339 | func | `KV` | KV prints indented key/value pairs with clean label alignment. |
| 345 | func | `Row` | Row prints an indented content line without arbitrary clipping. |
| 350 | func | `BlankRow` | BlankRow prints an empty line. |
| 354 | func | `max` | max |
| 362 | func | `Success` | Success / Warn / Error / Info one-liners for CLI. |
| 366 | func | `Warn` | warn |
| 370 | func | `Error` | error |
| 374 | func | `Info` | info |
| 380 | func | `Printf` | Printf is fmt.Printf with no styling (helper for consistency). |

## `pkg/tools` — 7 files · 62 funcs

Keywords: tools, pkg

### `pkg/tools/claude.go` (6 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 12 | type | `ClaudeTool` | ClaudeTool is the tools[] entry Claude Code sends on /v1/messages. |
| 19 | type | `ClaudeToolUseBlock` | ClaudeToolUseBlock is a content block type=tool_use. |
| 27 | func | `ParseClaudeTools` | ParseClaudeTools extracts tool defs from a Claude Code / Anthropic body. |
| 49 | func | `ToClaudeTools` | ToClaudeTools converts canonical defs to Claude Code tools[]. |
| 62 | func | `ToClaudeToolUseBlocks` | ToClaudeToolUseBlocks converts calls to Anthropic content blocks for SSE/JSON. |
| 80 | func | `FromClaudeToolUseBlocks` | FromClaudeToolUseBlocks extracts tool calls from Anthropic content blocks. |

### `pkg/tools/codex.go` (9 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 15 | type | `CodexResponsesTool` | CodexResponsesTool is the flat Responses API tools[] entry Codex CLI and chatgpt.com/backend-api/codex/responses speak (name at the top level, not nested under… |
| 23 | func | `ParseCodexTools` | ParseCodexTools extracts tool defs from a Codex OpenAI chat.completions body. |
| 30 | func | `ParseCodexResponsesTools` | ParseCodexResponsesTools extracts defs from a Responses API tools[] value. Accepts both the flat Codex/Responses shape and nested Chat Completions {"type":"fun… |
| 69 | func | `ToCodexTools` | ToCodexTools converts canonical defs to Codex/OpenAI chat.completions tools[]. |
| 74 | func | `ToCodexResponsesTools` | ToCodexResponsesTools converts canonical defs to Codex Responses tools[]. |
| 88 | func | `ToCodexToolCalls` | ToCodexToolCalls maps canonical calls to Codex chat.completions tool_calls. |
| 93 | func | `FromCodexToolCalls` | FromCodexToolCalls maps Codex chat.completions tool_calls to canonical. |
| 99 | func | `MarshalCodexChatRequest` | MarshalCodexChatRequest encodes req for an OpenAI-compatible upstream when the client dialect is Codex (full-context agent transcripts). |
| 108 | func | `MarshalCodexResponsesRequest` | MarshalCodexResponsesRequest encodes req for the Codex Responses backend (chatgpt.com/backend-api/codex/responses). Client tools (Claude input_schema, AGY func… |

### `pkg/tools/cursor.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 6 | func | `ParseCursorTools` | ParseCursorTools extracts tool defs from a Cursor OpenAI chat.completions body. |
| 11 | func | `ToCursorTools` | ToCursorTools converts canonical defs to Cursor/OpenAI tools[]. |
| 16 | func | `ToCursorToolCalls` | ToCursorToolCalls maps canonical calls to Cursor tool_calls. |
| 21 | func | `FromCursorToolCalls` | FromCursorToolCalls maps Cursor tool_calls to canonical. |
| 27 | func | `MarshalCursorChatRequest` | MarshalCursorChatRequest encodes req for an OpenAI-compatible upstream when the client dialect is Cursor. |

### `pkg/tools/dialect.go` (0 funcs)

- *(no exported/top-level funcs detected)*

### `pkg/tools/gemini.go` (6 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | type | `GeminiFunctionDeclaration` | GeminiFunctionDeclaration is the Gemini / Antigravity functionDeclarations[] shape. |
| 20 | type | `GeminiFunctionCall` | GeminiFunctionCall is a model-emitted functionCall part. |
| 26 | func | `ToGeminiFunctions` | ToGeminiFunctions converts canonical defs to Gemini functionDeclarations. |
| 39 | func | `FromGeminiFunctions` | FromGeminiFunctions maps Gemini declarations to canonical defs. |
| 55 | func | `FromGeminiFunctionCalls` | FromGeminiFunctionCalls maps Gemini functionCall parts to canonical. |
| 69 | func | `ToGeminiFunctionCalls` | ToGeminiFunctionCalls maps canonical calls to Gemini functionCall parts. |

### `pkg/tools/openai.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | type | `openAITool` | openAITool is the shared OpenAI tools[] wire shape (Cursor + Codex + openai_compatible upstream adapters). |
| 23 | type | `OpenAIToolCall` | OpenAIToolCall is the assistant.tool_calls[] wire shape. |
| 32 | type | `openAIChatRequest` | type/class openAIChatRequest |
| 42 | func | `parseOpenAITools` | parse open ai tools |
| 64 | func | `toOpenAITools` | to open ai tools |
| 78 | func | `ToOpenAIToolCalls` | ToOpenAIToolCalls maps canonical calls to OpenAI tool_calls. |
| 95 | func | `FromOpenAIToolCalls` | FromOpenAIToolCalls maps OpenAI tool_calls to canonical. |
| 107 | func | `normalizeOpenAIToolChoice` | normalize open ai tool choice |
| 136 | func | `isReasoningModel` | is reasoning model |
| 141 | func | `toOpenAIChatRequest` | to open ai chat request |
| 186 | func | `MarshalOpenAIChatRequest` | MarshalOpenAIChatRequest encodes a canonical request for any OpenAI-compatible upstream (shared by Cursor, Codex, and pool adapters). |

### `pkg/tools/webloop.go` (25 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 84 | func | `WebPreamble` | WebPreamble is appended to a web-backend prompt when the client sent tools[]. |
| 99 | func | `catalogLine` | catalogLine is "Name" or "Name:req1,req2" — live tools[], no hardcoded list. |
| 107 | func | `schemaKeys` | schema keys |
| 137 | func | `WebCloser` | WebCloser is appended after the flattened transcript so it outranks Claude Code's native tool-harness instructions. |
| 144 | func | `MaybeWrapWebStream` | MaybeWrapWebStream parses a text-only web stream into ToolCalls when the client sent tools[]. Logs extracted tools, then forwards them so Claude Code / Cursor … |
| 151 | func | `wrapWebStream` | wrap web stream |
| 221 | func | `FormatToolCalls` | FormatToolCalls is a one-line preview: `Bash {"command":"ls"} · Read {"path":"a"}`. |
| 240 | func | `isWebToolRefusal` | is web tool refusal |
| 271 | func | `isWebTitleJSON` | is web title json |
| 282 | func | `isWebWorkIncomplete` | is web work incomplete |
| 306 | func | `isWebFakeExecution` | is web fake execution |
| 330 | func | `shouldForceWebTools` | should force web tools |
| 341 | func | `filesFromHistory` | files from history |
| 362 | func | `historyHasTools` | history has tools |
| 371 | func | `findToolDef` | find tool def |
| 380 | func | `extractForcedTools` | extract forced tools |
| 494 | func | `hasBashCommand` | has bash command |
| 505 | func | `hasToolCall` | has tool call |
| 540 | func | `fallbackExploreTools` | fallbackExploreTools emits the same first moves an API-key model would: Read + Bash from the live catalog so Claude Code executes locally. |
| 559 | func | `toolArgKey` | tool arg key |
| 577 | func | `logWebTools` | log web tools |
| 591 | func | `ParseWebTools` | ParseWebTools extracts tool calls from a web model's text reply. |
| 684 | func | `repairJSON` | repair json |
| 725 | func | `parseToolCallJSON` | parse tool call json |
| 769 | func | `StripWebToolMarkup` | StripWebToolMarkup removes protocol / bash fences so Claude Code does not also print the commands as assistant text. |

## `pkg/types` — 6 files · 35 funcs

Keywords: types, pkg

### `pkg/types/account_id.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 11 | func | `EmailLocalPart` | EmailLocalPart returns the sanitized lower-case local part of an email (chars before @, letters+digits only). "Ninh.Le@x.com" → "ninhle". |
| 17 | func | `EmailDomainPart` | EmailDomainPart returns the sanitized domain of an email (chars after @, letters+digits only). "a@gmail.com" → "gmailcom". |
| 21 | func | `emailLocalRaw` | email local raw |
| 32 | func | `emailDomainRaw` | email domain raw |
| 40 | func | `sanitizeEmailSegment` | sanitize email segment |
| 53 | func | `AccountNamedID` | AccountNamedID builds a stable pool ID from brand, method, and email: "claude", "web", "ninhle@x.com" → "claude:web:ninhle". Returns "" when the email has no… |
| 59 | func | `AccountNamedIDWithDomain` | AccountNamedIDWithDomain disambiguates same local-parts across domains: "claude", "web", "ninhle@gmail.com" → "claude:web:ninhle-gmailcom". |
| 65 | func | `AccountBrandID` | AccountBrandID builds brand:local (no method segment) — ChatGPT web: "chatgpt", "ninhle@x.com" → "chatgpt:ninhle". |
| 70 | func | `AccountBrandIDWithDomain` | AccountBrandIDWithDomain → "chatgpt:ninhle-gmailcom". |
| 74 | func | `accountBrandID` | account brand id |
| 89 | func | `accountNamedID` | account named id |

### `pkg/types/chat.go` (6 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 22 | type | `ToolDef` | ToolDef is a provider-agnostic tool/function declaration. |
| 29 | type | `ToolCall` | ToolCall is one model-requested tool invocation. |
| 43 | type | `ChatMessage` | ChatMessage is one turn in a conversation. Roles: - "system", "user", "assistant" — normal chat - "tool" — tool result (OpenAI-shaped); set ToolCallID Assi… |
| 53 | type | `ChatRequest` | ChatRequest is provider-agnostic; each adapter translates it into whatever wire format its backend expects. |
| 89 | type | `StreamChunk` | StreamChunk is one piece of a streamed reply. The producer closes the channel after sending a chunk with Done set (or one carrying Error). |
| 103 | type | `ProviderAdapter` | ProviderAdapter is implemented by every chat backend the router can dispatch to. |

### `pkg/types/config.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 12 | func | `BaseDir` | BaseDir returns the root storage directory (~/.am, or overridden via $AM_HOME — the original var name — or $AM_DIR, checked second so either still works). |
| 24 | func | `CurrentUser` | CurrentUser returns the current OS username. |
| 32 | type | `ToolConfig` | ToolConfig describes all configured tools and their artifacts. |
| 37 | func | `DefaultToolConfig` | DefaultToolConfig returns the built-in tool specifications for Claude, Codex, and Gemini. |

### `pkg/types/id.go` (5 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | func | `FormatID` | FormatID renders a prefix and a 1-based index into the unified ID format, zero-padding the number to at least 2 digits. Examples: FormatID("gemini:api", 1) →… |
| 28 | func | `ParseID` | ParseID splits a unified-format ID back into its prefix and number. ok is false if id doesn't match (legacy literals, custom api-add names, named identity IDs … |
| 60 | func | `MigrateCompactID` | MigrateCompactID rewrites one ID from flat prefix form to brand:method form. "geminiapi:01" → "gemini:api:01". Named IDs (claude:web:ninhle) and custom names… |
| 93 | func | `RemapAccountIDsInText` | RemapAccountIDsInText rewrites old compact account IDs embedded in free text (events.log messages like "active provider → geminiapi:01"). |
| 124 | func | `DisplayAccountID` | DisplayAccountID returns the migrated form of an account ID for UI display. |

### `pkg/types/profile.go` (6 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 6 | type | `Artifact` | Artifact is one file or keychain entry that belongs to a tool's login state. |
| 22 | type | `ToolSpec` | ToolSpec describes how to snapshot / restore one CLI's auth state. |
| 28 | type | `ProfileMeta` | ProfileMeta represents metadata for a stored profile bundle on disk. |
| 44 | type | `ProfileEntry` | ProfileEntry is an artifact payload within a profile bundle. |
| 55 | type | `Window` | Window is one Anthropic rate-limit window (5h or 7d), straight from the anthropic-ratelimit-unified-{5h,7d}-{utilization,reset} headers. This is the same data … |
| 62 | type | `Token` | Token holds in-memory OAuth credentials and rate-limit states. |

### `pkg/types/usage.go` (3 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 6 | type | `UsageEntry` | UsageEntry records token usage for a request/response turn through the proxy. |
| 17 | type | `EventEntry` | EventEntry is a tagged realtime log line (rotate, failover, proxy, …). |
| 24 | type | `RequestEntry` | RequestEntry captures one chat turn's input/output for logging and error diagnosis. |

## `pkg/ui` — 5 files · 74 funcs

Keywords: ui, pkg, frontend, component

### `pkg/ui/account_groups.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | type | `accountRow` | accountRow is one printable line for accounts / pool / ls tables. |
| 28 | func | `groupSectionTitle` | group section title |
| 32 | func | `printGroupSection` | print group section |
| 36 | func | `printDisplaySection` | print display section |
| 41 | func | `apiProviderBrand` | apiProviderBrand extracts the vendor from a unified API id (gemini:api:01 → gemini). |
| 78 | func | `withAPIProvider` | with api provider |
| 85 | func | `apiSectionTitle` | api section title |
| 94 | func | `sortAccountRowsByGroup` | sortAccountRowsByGroup orders by GroupPriority, then API provider, then priority, then id. |
| 115 | func | `forEachAccountGroup` | forEachAccountGroup walks rows by GroupPriority. API Other splits into one section per provider (API · GEMINI, API · GROQ, …). |
| 152 | func | `forEachAPIProvider` | for each api provider |
| 175 | func | `matchesAccountRowFilter` | matchesAccountRowFilter decides whether a row appears under am accounts/ls [filter]. |

### `pkg/ui/login.go` (28 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 26 | type | `loginFlags` | loginFlags holds optional non-interactive credentials passed on the CLI. |
| 36 | func | `parseLoginFlags` | parse login flags |
| 80 | func | `CmdLogin` | CmdLogin handles login. Default for chatgpt/claude: open a dedicated browser window, you sign in on the website, we capture the session cookie via CDP (no Keyc… |
| 150 | func | `readLinePrompt` | read line prompt |
| 157 | func | `nextPoolID` | next pool id |
| 177 | func | `loginChatGPT` | login chat gpt |
| 269 | func | `loginClaude` | login claude |
| 361 | func | `coalesceModel` | coalesce model |
| 370 | func | `savePoolLogin` | savePoolLogin resolves the pool slot (same account → relogin) and persists credentials under a stable identity ID when email is known. |
| 389 | func | `loginGemini` | login gemini |
| 436 | func | `loginGeminiWeb` | login gemini web |
| 495 | func | `loginGitHubModels` | login git hub models |
| 530 | type | `openAICompatSpec` | type/class openAICompatSpec |
| 539 | func | `loginOpenAICompat` | login open ai compat |
| 574 | func | `loginGroq` | login groq |
| 585 | func | `loginKimi` | login kimi |
| 596 | func | `loginGrok` | login grok |
| 609 | func | `CmdDoctorProviders` | CmdDoctorProviders live-probes every pool adapter with a tiny chat turn and prints OK/FAIL. Used to answer "does chatgpt/gemini/api actually work?". |
| 658 | func | `providerKindLabel` | provider kind label |
| 680 | func | `poolMark` | pool mark |
| 687 | func | `loadProviderRows` | load provider rows |
| 710 | func | `CmdAccounts` | CmdAccounts lists every saved account: Claude profiles + web/API providers. |
| 716 | func | `CmdAccountsFilter` | CmdAccountsFilter lists accounts grouped by rotate priority. filter may be a tool/group hint (claude, codex, agy, api, web) or empty = all. |
| 741 | func | `collectAccountRows` | collect account rows |
| 818 | func | `CmdPool` | CmdPool lists accounts currently in the rotate pool (POOL=IN). |
| 841 | func | `collectPoolRows` | collect pool rows |
| 885 | func | `CmdAccountsCmd` | CmdAccountsCmd handles `am accounts [priority <id> <N>]`. |
| 965 | func | `CmdAPI` | CmdAPI handles 'am api add', 'am api rm', 'am api ls'. |

### `pkg/ui/picker.go` (2 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | func | `PickProfile` | PickProfile shows an arrow-key menu of profiles/providers and returns the chosen name. |
| 127 | func | `providerMenuEntries` | provider menu entries |

### `pkg/ui/status.go` (19 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 21 | type | `proxyStatus` | proxyStatus is the decoded /_am/status payload. |
| 32 | type | `statusAccount` | type/class statusAccount |
| 49 | func | `fetchProxyStatus` | fetch proxy status |
| 64 | func | `CmdStatus` | CmdStatus displays a modern realtime dashboard for proxy, Claude accounts, and the multi-provider pool. |
| 70 | func | `printStatusBody` | printStatusBody renders full account/pool detail (Accounts tab + `amux status`). |
| 95 | func | `printProxyKV` | print proxy kv |
| 115 | func | `proxyPort` | proxy port |
| 123 | func | `splitHostPort` | split host port |
| 127 | func | `printClaudeAccountsDetail` | print claude accounts detail |
| 150 | func | `claudeAccountPlan` | claude account plan |
| 160 | func | `printOneClaudeAccount` | print one claude account |
| 234 | func | `printPoolDetail` | print pool detail |
| 261 | func | `printOnePoolAccount` | print one pool account |
| 304 | func | `printAutoUpdate` | print auto update |
| 312 | func | `printLimitBar` | print limit bar |
| 321 | func | `anyLastUsed` | any last used |
| 330 | func | `resetSuffix` | reset suffix |
| 350 | func | `printGuardDetail` | print guard detail |
| 390 | func | `CmdGuard` | CmdGuard displays anti-ban protection status or resets account health states. |

### `pkg/ui/statusline.go` (14 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 18 | type | `StatuslineInput` | StatuslineInput is JSON Claude Code / AGY pipe to a statusLine command. |
| 24 | type | `statuslineContext` | type/class statuslineContext |
| 35 | type | `statuslineQuota` | type/class statuslineQuota |
| 40 | type | `statuslineRates` | type/class statuslineRates |
| 51 | type | `LimitWindows` | LimitWindows is 0–1 utilization from the proxy rotator when the client omits rate_limits / quota (API-key mode). |
| 57 | func | `CmdStatusline` | CmdStatusline renders session tokens + 5h / 7d remaining. |
| 62 | func | `ReadStatuslineInput` | read statusline input |
| 69 | func | `RenderStatusline` | RenderStatusline: `10k 5h [bar] N%left 7d [bar] N%left` — no window size. |
| 80 | func | `sessionTokens` | session tokens |
| 105 | func | `writeLimit` | write limit |
| 125 | func | `mergeLimits` | merge limits |
| 158 | func | `limitsFromQuota` | limits from quota |
| 181 | func | `quotaUsed` | quota used |
| 199 | func | `fetchProxyLimits` | fetch proxy limits |

## `pkg/usage` — 4 files · 29 funcs

Keywords: usage, pkg

### `pkg/usage/capture.go` (7 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 20 | func | `UsageLogPath` | usage log path |
| 22 | func | `AppendUsageEntry` | append usage entry |
| 36 | type | `closeBothCloser` | type/class closeBothCloser |
| 41 | method | `Close` | close |
| 51 | func | `WrapUsageCapture` | WrapUsageCapture tees resp.Body through a parser that pulls out token counts. |
| 121 | func | `parseUsageStream` | parse usage stream |
| 144 | func | `applyUsageJSON` | apply usage json |

### `pkg/usage/project.go` (7 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 13 | type | `projectCache` | type/class projectCache |
| 19 | type | `cachedProject` | type/class cachedProject |
| 32 | func | `ProjectForRemoteAddr` | ProjectForRemoteAddr resolves a client connection's cwd from RemoteAddr. |
| 53 | func | `lookupProjectDir` | lookup project dir |
| 73 | func | `gitRootOrSelf` | gitRootOrSelf resolves dir to its enclosing git repository root, so usage run from a subdirectory of a repo (rather than the repo's own top level) is still att… |
| 85 | func | `lookupClientPID` | lookup client pid |
| 104 | func | `lookupCwd` | lookup cwd |

### `pkg/usage/usage.go` (11 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 16 | func | `LoadUsageEntries` | load usage entries |
| 42 | type | `usageAgg` | type/class usageAgg |
| 49 | func | `PrintUsageReport` | PrintUsageReport parses flags and prints either the daily aggregate table or the detailed breakdown by account/model/project/session. |
| 148 | func | `printUsageByDay` | print usage by day |
| 173 | func | `printUsageDetail` | print usage detail |
| 219 | func | `ProjectLabel` | project label |
| 226 | func | `SessionLabel` | session label |
| 236 | func | `bump` | bump |
| 247 | func | `printUsageTable` | print usage table |
| 259 | func | `printUsageTableByTime` | print usage table by time |
| 272 | func | `Commas` | commas |

### `pkg/usage/window.go` (4 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 6 | func | `FormatTokens` | FormatTokens renders a compact token count: 0, 850, 12k, 200k, 1M. |
| 25 | func | `itoa` | itoa |
| 47 | func | `trimFloat` | trim float |
| 55 | func | `sprintf1` | sprintf1 |

## `pkg/utils` — 1 files · 1 funcs

Keywords: utils, pkg

### `pkg/utils/schema.go` (1 funcs)

| Line | Kind | Name | Summary |
|-----:|------|------|---------|
| 9 | func | `NormalizeJSONSchema` | NormalizeJSONSchema returns a usable JSON Schema object. Empty or null input becomes {"type":"object","properties":{}}. |

