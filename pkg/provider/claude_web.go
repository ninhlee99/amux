package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/browser"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

type ClaudeWebAdapter struct {
	AdapterID   string
	PriorityLvl int
	SessionKey  string
	Cookies     string // full Cookie header from login CDP (preferred over SessionKey alone)
	TargetModel string
	PlanTier    string // "pro" | "max" | "team" | "free" | …
	HTTPClient  *http.Client

	// Cached across turns in one am chat / pool process so we don't burn
	// Claude's free-tier request budget creating a new conversation (and
	// re-fetching org) on every message.
	mu              sync.Mutex
	orgID           string
	convMgr         *ProjectConversationManager
	cookieRefreshed bool // one CDP jar refresh per adapter lifetime (or after 429)
	planChecked     bool // one org-capability re-detect when plan looks free/stale
}

func (a *ClaudeWebAdapter) convs() *ProjectConversationManager {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.convMgr == nil {
		a.convMgr = NewProjectConversationManager(a.AdapterID, DefaultMaxTurnsPerConversation, DefaultConversationTTL)
	}
	return a.convMgr
}

const (
	claudeWebConversationBase = "https://claude.ai/api/organizations/%s/chat_conversations"
	claudeWebDefaultModel     = "claude-sonnet-5"
)

// claudeWebOrganizationsURL is a var (not const) so tests can point it at an
// httptest server instead of the real claude.ai endpoint.
var claudeWebOrganizationsURL = "https://claude.ai/api/organizations"

func (a *ClaudeWebAdapter) ID() string    { return a.AdapterID }
func (a *ClaudeWebAdapter) Priority() int { return a.PriorityLvl }
func (a *ClaudeWebAdapter) Plan() string  { return a.PlanTier }

// SupportsTools is false: claude.ai chat has no Anthropic tool_use wire.
func (a *ClaudeWebAdapter) SupportsTools() bool { return false }

func (a *ClaudeWebAdapter) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

func (a *ClaudeWebAdapter) model() string {
	m := a.TargetModel
	if m == "" {
		m = claudeWebDefaultModel
	}
	// Unless user explicitly requested Opus via ANTHROPIC_MODEL env, never upgrade to Opus to preserve quota.
	envModel := strings.ToLower(strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")))
	if !strings.Contains(envModel, "opus") && strings.Contains(strings.ToLower(m), "opus") {
		return claudeWebDefaultModel
	}
	// Free plan cannot sustain Opus — keep Sonnet to avoid soft-ban / quality cliff.
	if strings.EqualFold(strings.TrimSpace(a.PlanTier), "free") &&
		strings.Contains(strings.ToLower(m), "opus") {
		return claudeWebDefaultModel
	}
	return m
}

func (a *ClaudeWebAdapter) cookieHeader() string {
	if strings.TrimSpace(a.Cookies) != "" {
		return a.Cookies
	}
	return "sessionKey=" + a.SessionKey
}

func claudeCookiesHaveClearance(cookieHeader string) bool {
	h := strings.ToLower(cookieHeader)
	return strings.Contains(h, "cf_clearance=") || strings.Contains(h, "__cf_bm=")
}

// refreshCookiesFromProfile pulls Cloudflare cookies from the existing
// ~/.am/browser-profiles/claude jar. Bare sessionKey hits a bot rate bucket
// where turn 2 429s while the real browser multi-turns fine.
func (a *ClaudeWebAdapter) refreshCookiesFromProfile() error {
	a.mu.Lock()
	if a.cookieRefreshed && claudeCookiesHaveClearance(a.Cookies) {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	auth, err := browser.RefreshWebAuthFromProfile(browser.ClaudeWebLogin, 25*time.Second)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.Cookies = auth.CookieHeader
	if auth.SessionValue != "" {
		a.SessionKey = auth.SessionValue
	}
	a.cookieRefreshed = true
	a.mu.Unlock()

	if err := UpdateProviderCookies(DefaultAccountsPath(), a.AdapterID, auth.SessionValue, auth.CookieHeader); err != nil {
		log.Printf("%s: refreshed cookies but failed to persist: %v", a.AdapterID, err)
	} else {
		log.Printf("%s: refreshed Cloudflare cookie jar from browser profile", a.AdapterID)
	}
	a.refreshPlanAndModel()
	return nil
}

// refreshPlanAndModel re-detects tier/model so skipFreeWebHardTask doesn't
// treat a Max account as free after a stale accounts.json plan field.
func (a *ClaudeWebAdapter) refreshPlanAndModel() {
	model, plan := DetectClaudeWebAccount(a.SessionKey, a.Cookies)
	if model == "" && plan == "" {
		return
	}
	a.mu.Lock()
	if model != "" {
		a.TargetModel = model
	}
	if plan != "" {
		a.PlanTier = plan
	}
	id, m, p := a.AdapterID, a.TargetModel, a.PlanTier
	a.mu.Unlock()
	if err := UpdateProviderPlanModel(DefaultAccountsPath(), id, p, m); err != nil {
		log.Printf("%s: refreshed plan/model but failed to persist: %v", id, err)
	}
}

func (a *ClaudeWebAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if a.SessionKey == "" && strings.TrimSpace(a.Cookies) == "" {
		return nil, fmt.Errorf("%s: %w: no sessionKey configured", a.AdapterID, types.ErrAuthentication)
	}

	// Bare sessionKey (no CF jar) → harsh bot bucket; browser multi-turns OK.
	if !claudeCookiesHaveClearance(a.cookieHeader()) {
		_ = a.refreshCookiesFromProfile()
	}

	// Stale accounts.json may mark Max as free — re-detect once per process.
	a.mu.Lock()
	needPlan := !a.planChecked && (a.PlanTier == "" || strings.EqualFold(a.PlanTier, "free"))
	if needPlan {
		a.planChecked = true
	}
	a.mu.Unlock()
	if needPlan {
		a.refreshPlanAndModel()
	}

	model := a.model()
	const maxAttempts = 4
	project := req.Project()
	cm := a.convs()
	var resp *http.Response
	refreshedFor429 := false
	rotatedConv := false
	for attempt := 0; attempt < maxAttempts; attempt++ {
		activeConv, hasActive := cm.GetActive(project)
		hasValidThread := hasActive && activeConv.ID != ""

		// After rotate, server thread is empty → force full flatten (not delta).
		prompt := WebBackendPrompt(req, hasValidThread && !rotatedConv)

		payloadMap := map[string]any{
			"prompt":      prompt,
			"model":       model,
			"timezone":    "Asia/Ho_Chi_Minh",
			"attachments": []any{},
			"files":       []any{},
		}
		b, err := json.Marshal(payloadMap)
		if err != nil {
			return nil, fmt.Errorf("%s: marshal: %w", a.AdapterID, err)
		}

		orgID, convUUID, err := a.ensureConversation(ctx, model, project, req.SessionID)
		if err != nil {
			return nil, err
		}
		chatURL := fmt.Sprintf("%s/%s/completion", fmt.Sprintf(claudeWebConversationBase, orgID), convUUID)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("%s: new request: %w", a.AdapterID, err)
		}
		httpReq.Header.Set("Cookie", a.cookieHeader())
		setClaudeWebHeaders(httpReq, true)

		resp, err = a.client().Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.AdapterID, err)
		}

		// Stale conversation on disk or server → drop and open a new thread once.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			resp.Body.Close()
			if !rotatedConv {
				rotatedConv = true
				cm.ResetProject(project)
				log.Printf("%s: conversation gone for project %s — starting a new Claude thread", a.AdapterID, project)
				continue
			}
			return nil, fmt.Errorf("%s: conversation not found after rotate", a.AdapterID)
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			break
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		retryAfter := parseRetryAfterSeconds(resp.Header)
		resp.Body.Close()

		if retryAfter <= 0 && !refreshedFor429 {
			refreshedFor429 = true
			a.mu.Lock()
			a.cookieRefreshed = false
			a.mu.Unlock()
			if a.refreshCookiesFromProfile() == nil {
				log.Printf("%s: 429 Retry-After=0 — refreshed CF cookies, retrying", a.AdapterID)
				continue
			}
		}

		// Plan/bot limit on this thread → open a fresh conversation and retry once.
		if !rotatedConv {
			rotatedConv = true
			cm.ResetProject(project)
			log.Printf("%s: rate limited on current thread for project %s — starting a new Claude conversation", a.AdapterID, project)
			if retryAfter > 0 {
				wait := time.Duration(retryAfter) * time.Second
				if wait > 2*time.Minute {
					wait = 2 * time.Minute
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		}

		return nil, claudeFreeRateLimitErr(a.AdapterID, body, retryAfter)
	}

	if err := a.mapClaudeHTTPError(resp); err != nil {
		return nil, err
	}

	out := make(chan types.StreamChunk)
	go streamClaudeWeb(ctx, a.AdapterID, resp, out)
	return tools.MaybeWrapWebStream(a.AdapterID, req, out), nil
}

// ensureConversation reuses the server-side Claude conversation for the specific project if within turn limits.
func (a *ClaudeWebAdapter) ensureConversation(ctx context.Context, model, project, sessionID string) (orgID, convUUID string, err error) {
	a.mu.Lock()
	if a.orgID == "" {
		id, e := a.getOrganizationID(ctx)
		if e != nil {
			a.mu.Unlock()
			return "", "", e
		}
		a.orgID = id
	}
	orgID = a.orgID
	a.mu.Unlock()

	cm := a.convs()
	if c, ok := cm.GetActive(project); ok && c.ID != "" {
		cm.Register(project, sessionID, c.ID, "", nil)
		return orgID, c.ID, nil
	}

	id, e := a.createConversation(ctx, orgID, model)
	if e != nil {
		return "", "", e
	}
	cm.Register(project, sessionID, id, "", nil)
	return orgID, id, nil
}

// ResetConversation clears all server-side Claude.ai threads.
func (a *ClaudeWebAdapter) ResetConversation() {
	a.convs().ResetAll()
}

// ResetConversationForScope clears the server-side Claude.ai thread for a specific project.
func (a *ClaudeWebAdapter) ResetConversationForScope(scopeKey string) {
	a.convs().ResetProject(scopeKey)
}

func lastUserPrompt(messages []types.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") && messages[i].Content != "" {
			return messages[i].Content
		}
	}
	return BuildConcatenatedPrompt(messages)
}

func (a *ClaudeWebAdapter) mapClaudeHTTPError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		retryAfter := parseRetryAfterSeconds(resp.Header)
		resp.Body.Close()
		return claudeFreeRateLimitErr(a.AdapterID, b, retryAfter)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return types.ErrAuthentication
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
	resp.Body.Close()
	msg := string(bytes.TrimSpace(b))
	if resp.StatusCode == http.StatusForbidden && strings.Contains(msg, "model") {
		return fmt.Errorf("%s: status %d: %s", a.AdapterID, resp.StatusCode, msg)
	}
	if resp.StatusCode == http.StatusForbidden {
		return types.ErrAuthentication
	}
	// Stale conversation — drop cache so the next turn opens a new one.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		a.ResetConversation()
	}
	return fmt.Errorf("%s: status %d: %s", a.AdapterID, resp.StatusCode, msg)
}

func parseRetryAfterSeconds(h http.Header) int {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func claudeFreeRateLimitErr(id string, body []byte, retryAfter int) error {
	hint := "Claude free/plan message limit — wait a few minutes, use another provider (geminiapi/chatgptweb), or upgrade Pro"
	if retryAfter > 0 {
		hint = fmt.Sprintf("%s (Retry-After=%ds)", hint, retryAfter)
	}
	msg := string(bytes.TrimSpace(body))
	if msg == "" {
		return fmt.Errorf("%s: %w: %s", id, types.ErrRateLimitReached, hint)
	}
	return fmt.Errorf("%s: %w: %s — %s", id, types.ErrRateLimitReached, hint, msg)
}

func setClaudeWebHeaders(req *http.Request, includeJSON bool) {
	if includeJSON {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Referer", "https://claude.ai/new")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("anthropic-client-platform", "web_claude_ai")
}

func (a *ClaudeWebAdapter) getOrganizationID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeWebOrganizationsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", a.cookieHeader())
	req.Header.Set("Accept", "application/json")
	setClaudeWebHeaders(req, false)

	resp, err := a.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: get org: %w", a.AdapterID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", claudeFreeRateLimitErr(a.AdapterID, b, parseRetryAfterSeconds(resp.Header))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", types.ErrAuthentication
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		if resp.StatusCode == http.StatusForbidden {
			msg := string(bytes.TrimSpace(b))
			if msg == "" || strings.Contains(msg, "Just a moment") {
				return "", types.ErrAuthentication
			}
			return "", fmt.Errorf("%s: get org status %d: %s", a.AdapterID, resp.StatusCode, msg)
		}
		return "", fmt.Errorf("%s: get org returned status %d: %s", a.AdapterID, resp.StatusCode, bytes.TrimSpace(b))
	}

	var orgs []struct {
		UUID string `json:"uuid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return "", fmt.Errorf("%s: decode orgs: %w", a.AdapterID, err)
	}
	if len(orgs) == 0 || orgs[0].UUID == "" {
		return "", fmt.Errorf("%s: no organization found", a.AdapterID)
	}
	return orgs[0].UUID, nil
}

// DetectClaudeWebModel queries claude.ai/api/organizations with a signed-in
// session and returns the best model that account's plan capabilities allow
// (Opus for pro/max/team, else Sonnet), or "" if capabilities can't be
// determined — callers should fall back to their own hardcoded default,
// never blocking login on this.
func DetectClaudeWebModel(sessionKey, cookieHeader string) string {
	model, _ := DetectClaudeWebAccount(sessionKey, cookieHeader)
	return model
}

// DetectClaudeWebAccount returns best model + plan tier from organizations API.
// Plan is "max" | "pro" | "free" (empty on network/auth failure).
func DetectClaudeWebAccount(sessionKey, cookieHeader string) (model, plan string) {
	cookie := strings.TrimSpace(cookieHeader)
	sessionKey = strings.TrimSpace(sessionKey)
	if cookie == "" {
		if sessionKey == "" {
			return "", ""
		}
		cookie = "sessionKey=" + sessionKey
	} else if sessionKey != "" && !strings.Contains(cookie, "sessionKey=") {
		cookie = "sessionKey=" + sessionKey + "; " + cookie
	}

	req, err := http.NewRequest(http.MethodGet, claudeWebOrganizationsURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	setClaudeWebHeaders(req, false)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	var orgs []struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil || len(orgs) == 0 {
		return "", ""
	}
	caps := orgs[0].Capabilities
	return pickClaudeWebModel(caps), pickClaudeWebPlan(caps)
}

// pickClaudeWebModel maps an organization's capabilities (from
// claude.ai/api/organizations) to the model for web conversations.
// We default to Sonnet (claudeWebDefaultModel) even for Pro/Team to prevent
// extreme rate-limit burn from Opus on large contexts.
func pickClaudeWebModel(capabilities []string) string {
	for _, cap := range capabilities {
		switch cap {
		case "claude_max", "raven":
			// Reserved for max tier if explicit, otherwise Sonnet is safer
			return claudeWebDefaultModel
		}
	}
	return claudeWebDefaultModel
}

// pickClaudeWebPlan maps org capabilities to a non-free tier when paid.
func pickClaudeWebPlan(capabilities []string) string {
	for _, cap := range capabilities {
		switch cap {
		case "claude_max", "raven":
			return "max"
		case "claude_pro", "claude_team", "claude_enterprise":
			return "pro"
		}
	}
	return "free"
}

func (a *ClaudeWebAdapter) createConversation(ctx context.Context, orgID, model string) (string, error) {
	url := fmt.Sprintf(claudeWebConversationBase, orgID)
	payloadMap := map[string]string{
		"uuid": newUUIDv4(),
		"name": "",
	}
	if model != "" {
		payloadMap["model"] = model
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return "", fmt.Errorf("%s: marshal create conv: %w", a.AdapterID, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", a.cookieHeader())
	setClaudeWebHeaders(req, false)

	resp, err := a.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: create conv: %w", a.AdapterID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", claudeFreeRateLimitErr(a.AdapterID, b, parseRetryAfterSeconds(resp.Header))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", types.ErrAuthentication
	}
	if resp.StatusCode == http.StatusForbidden {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		msg := string(bytes.TrimSpace(b))
		if strings.Contains(msg, "model") {
			return "", fmt.Errorf("%s: create conv status %d: %s", a.AdapterID, resp.StatusCode, msg)
		}
		return "", types.ErrAuthentication
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", fmt.Errorf("%s: create conv returned status %d: %s", a.AdapterID, resp.StatusCode, bytes.TrimSpace(b))
	}

	var conv struct {
		UUID string `json:"uuid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conv); err != nil {
		return "", fmt.Errorf("%s: decode conv: %w", a.AdapterID, err)
	}
	if conv.UUID == "" {
		return "", fmt.Errorf("%s: empty conversation uuid", a.AdapterID)
	}
	return conv.UUID, nil
}

func streamClaudeWeb(ctx context.Context, id string, resp *http.Response, out chan<- types.StreamChunk) {
	defer close(out)
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 2<<20)

	doneSent := false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
			doneSent = true
			return
		}

		var chunk struct {
			Completion   string `json:"completion"`
			Thinking     string `json:"thinking"`
			StopReason   string `json:"stop_reason"`
			Error        any    `json:"error"`
			MessageLimit *struct {
				Type      string `json:"type"`
				ResetsAt  any    `json:"resetsAt"`
				Remaining any    `json:"remaining"`
			} `json:"messageLimit"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: %v", id, chunk.Error), Done: true})
			doneSent = true
			return
		}
		if chunk.MessageLimit != nil && chunk.MessageLimit.Type != "" && chunk.MessageLimit.Type != "within_limit" {
			log.Printf("%s: Claude messageLimit=%s resetsAt=%v remaining=%v",
				id, chunk.MessageLimit.Type, chunk.MessageLimit.ResetsAt, chunk.MessageLimit.Remaining)
		}
		if chunk.Thinking != "" {
			if !sendChunk(ctx, out, types.StreamChunk{ID: id, Thinking: chunk.Thinking}) {
				return
			}
		}
		if chunk.Completion != "" {
			if !sendChunk(ctx, out, types.StreamChunk{ID: id, Content: chunk.Completion}) {
				return
			}
		}
		if chunk.StopReason != "" {
			sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
			doneSent = true
			return
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		sendChunk(ctx, out, types.StreamChunk{ID: id, Error: fmt.Errorf("%s: read stream: %w", id, err), Done: true})
		return
	}
	if !doneSent && ctx.Err() == nil {
		sendChunk(ctx, out, types.StreamChunk{ID: id, Done: true})
	}
}
