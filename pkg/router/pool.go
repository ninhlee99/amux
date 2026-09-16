package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/ctxshrink"
	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/privacy"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
)

// redactBeforeSend is the universal outbound gate: every adapter path
// (proxy bridge, gateway, tests) must pass here before network I/O.
func redactBeforeSend(req *types.ChatRequest) {
	if !privacy.Enabled {
		return
	}
	res := privacy.RedactChatRequest(req)
	if res.Len() == 0 {
		return
	}
	dialect := "pool"
	if req != nil && req.ClientDialect != "" {
		dialect = req.ClientDialect
	}
	privacy.LogHits(nil, res, dialect)
}

// rateLimitCooldown is the base cooldown when upstream provides no Retry-After hint.
// Adaptive: if the upstream response carries a Retry-After header, that value
// is used directly (clamped to minAdaptiveCooldown..maxAdaptiveCooldown).
const (
	rateLimitCooldown    = 2 * time.Minute
	minAdaptiveCooldown  = 5 * time.Second
	maxAdaptiveCooldown  = 30 * time.Minute
)

// AccountPoolRouter dispatches a ChatRequest to the highest-priority
// adapter that isn't currently cooling down. A preferred adapter from
// `am sw <provider>` is tried first; rate-limit failover promotes the
// winner to preferred so status stays in sync. Non-rate-limit failures
// on a pin do not silently jump to another provider.
//
// directory holds every addressable adapter (including out-of-pool) for
// explicit X-Provider routing via SendNamed.
type AccountPoolRouter struct {
	adapters     []types.ProviderAdapter
	directory    map[string]types.ProviderAdapter
	preferred    string
	manualPin    bool // true only after `am sw` / SetPreferred — auto leftover must not steal new sessions
	lastUsed     string
	sessionRR    int // round-robin cursor for new-session load balance
	mu           sync.RWMutex
	cooldownMap  map[string]time.Time
	groupIndices map[string]int
}

// NewAccountPoolRouter builds a router over adapters, sorted once by
// Priority() ascending (1 tried first).
func NewAccountPoolRouter(adapters []types.ProviderAdapter) *AccountPoolRouter {
	sorted := make([]types.ProviderAdapter, len(adapters))
	copy(sorted, adapters)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority() < sorted[j].Priority() })
	dir := make(map[string]types.ProviderAdapter, len(sorted))
	for _, a := range sorted {
		dir[a.ID()] = a
	}
	return &AccountPoolRouter{
		adapters:     sorted,
		directory:    dir,
		cooldownMap:  make(map[string]time.Time),
		groupIndices: make(map[string]int),
	}
}

// SetDirectory replaces the explicit-routing map (may include adapters not
// in the rotate pool). Rotate order is unchanged.
func (r *AccountPoolRouter) SetDirectory(adapters []types.ProviderAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := make(map[string]types.ProviderAdapter, len(adapters))
	for _, a := range adapters {
		dir[a.ID()] = a
	}
	// Keep rotate adapters addressable too.
	for _, a := range r.adapters {
		dir[a.ID()] = a
	}
	r.directory = dir
}

// applyTaskClassification inspects the request and dynamically enables thinking
// mode or escalates to Pro tier when heavy analytical reasoning is required.
// Also stamps a soft TaskKind used only for account-group preference.
// Token-aware: coding/fix do not auto-think/pro unless client/model/crash signal.
func applyTaskClassification(req *types.ChatRequest) {
	if req == nil {
		return
	}
	c := ClassifyTask(req)
	if c.Kind != "" {
		req.TaskKind = c.Kind
	}

	explicit := classificationExplicit(c)
	if c.IsHeavy && req.TargetTier == "" {
		if shouldEscalatePro(c.Kind, explicit) {
			req.TargetTier = "pro"
			log.Printf("router: heavy task detected (%s) -> escalated to Pro tier", strings.Join(c.Reasons, ", "))
		}
	}
	if c.NeedsThinking && !req.Thinking {
		if shouldAutoThink(c.Kind, explicit) {
			req.Thinking = true
			if req.ReasoningEffort == "" {
				req.ReasoningEffort = c.ReasoningEffort
			}
			if req.ThinkingBudget <= 0 {
				req.ThinkingBudget = thinkingBudgetForKind(c.Kind)
			}
			log.Printf("router: auto-enabled thinking mode (effort: %s, budget: %d, reasons: %s)",
				req.ReasoningEffort, req.ThinkingBudget, strings.Join(c.Reasons, ", "))
		}
	}

	// Real compact path for TaskCompact — shrink mid-history before adapters.
	if req.TaskKind == TaskCompact && len(req.Messages) > 8 {
		req.Messages = ctxshrink.CompactTranscript(req.Messages)
	}
	maybeAutoTouchMap(req)
}

func classificationExplicit(c TaskClassification) bool {
	for _, r := range c.Reasons {
		low := strings.ToLower(r)
		if strings.Contains(low, "client requested") ||
			strings.Contains(low, "model name specifies") ||
			strings.Contains(low, "stack trace") {
			return true
		}
	}
	return false
}

func shouldEscalatePro(kind string, explicit bool) bool {
	switch kind {
	case TaskCoding, TaskFix:
		return explicit
	default:
		return true
	}
}

func shouldAutoThink(kind string, explicit bool) bool {
	switch kind {
	case TaskCoding, TaskFix:
		return explicit
	default:
		return true
	}
}

func thinkingBudgetForKind(kind string) int {
	switch kind {
	case TaskReview, TaskCompact, TaskCoding, TaskFix:
		return 512
	case TaskAnalysis, TaskQuality:
		return 1024
	default:
		return 1024
	}
}

// SendNamed routes to one adapter by ID (rotate pool or out-of-pool directory).
func (r *AccountPoolRouter) SendNamed(ctx context.Context, id string, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	applyTaskClassification(req)
	redactBeforeSend(req)
	r.mu.RLock()
	a := r.directory[id]
	r.mu.RUnlock()
	if a == nil {
		return nil, fmt.Errorf("provider %q not addressable (see: am accounts)", id)
	}
	isWeb := strings.Contains(id, "web") || strings.HasPrefix(id, "chatgpt")
	if err := guard.Pace(ctx, id, isWeb); err != nil {
		return nil, err
	}
	ch, err := a.SendMessageStream(ctx, req)
	if err != nil {
		guard.RecordError(id, err)
		return nil, err
	}
	guard.RecordSuccess(id)
	r.mu.Lock()
	r.lastUsed = id
	r.mu.Unlock()
	return ch, nil
}

// SetPreferred sets a MANUAL pin (`am sw <provider>`). New sessions stick
// to this adapter until cleared or failed over. Auto-failover leftovers
// must not call this — use markUsed(promote) which clears manualPin.
func (r *AccountPoolRouter) SetPreferred(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferred = id
	r.manualPin = strings.TrimSpace(id) != ""
}

// ClearPreferred drops the manual pin so session load-balance resumes.
func (r *AccountPoolRouter) ClearPreferred() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferred = ""
	r.manualPin = false
}

// ManualPin reports whether preferred came from `am sw` (not auto-switch).
func (r *AccountPoolRouter) ManualPin() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.manualPin
}

// HasLivingGroup reports whether at least one adapter belonging to any of the specified groups
// is configured in the pool and not currently cooling down or quarantined.
func (r *AccountPoolRouter) HasLivingGroup(groups ...string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	targetGroups := make(map[string]bool, len(groups))
	for _, g := range groups {
		targetGroups[g] = true
	}
	for _, a := range r.adapters {
		grp := DetermineAdapterGroup(a)
		if targetGroups[grp] {
			if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
				continue
			}
			if cd, exists := r.cooldownMap[a.ID()]; exists && time.Now().Before(cd) {
				continue
			}
			return true
		}
	}
	return false
}

// ConversationResetter is implemented by web adapters that keep a
// server-side chat thread. Called on `am sw` so the next turn does not
// continue an unrelated conversation.
type ConversationResetter interface {
	ResetConversation()
}

// ResetConversations clears server-side web threads on every adapter that
// supports it (Claude/ChatGPT/Gemini web). Safe to call when switching
// providers so Claude Code history is not mixed with an old UI chat.
func (r *AccountPoolRouter) ResetConversations() {
	r.mu.RLock()
	adapters := append([]types.ProviderAdapter(nil), r.adapters...)
	r.mu.RUnlock()
	for _, a := range adapters {
		if rr, ok := a.(ConversationResetter); ok {
			rr.ResetConversation()
		}
	}
}

// Preferred returns the currently preferred adapter ID.
func (r *AccountPoolRouter) Preferred() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preferred
}

// LastUsed returns the adapter ID that last successfully started a stream.
func (r *AccountPoolRouter) LastUsed() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastUsed
}

// Len returns how many adapters the pool currently holds (including
// auto-surfaced Codex fallback).
func (r *AccountPoolRouter) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adapters)
}

type toolCapability interface {
	SupportsTools() bool
}

func adapterSupportsTools(a types.ProviderAdapter) bool {
	if t, ok := a.(toolCapability); ok {
		return t.SupportsTools()
	}
	return true
}

// Client tool loops require native structured tool calls. Web adapters can
// generate text that resembles a call but cannot preserve execution semantics.
//
// WebPolicy controls when text-only (web) adapters may serve tools[]:
//   - last_resort (default): skip web when a native tool backend is usable
//   - prefer / force: allow web even when native exists (force = same skip rule off)
// Override via AM_WEB_POLICY or accounts.json "webPolicy".
const (
	WebPolicyLastResort = "last_resort"
	WebPolicyPrefer     = "prefer"
	WebPolicyForce      = "force"
)

var webPolicyMu sync.RWMutex
var webPolicy = WebPolicyLastResort

// SetWebPolicy updates runtime web routing policy (empty → last_resort).
func SetWebPolicy(policy string) {
	p := strings.ToLower(strings.TrimSpace(policy))
	switch p {
	case WebPolicyPrefer, WebPolicyForce, WebPolicyLastResort:
		// ok
	case "":
		p = WebPolicyLastResort
	default:
		p = WebPolicyLastResort
	}
	webPolicyMu.Lock()
	webPolicy = p
	webPolicyMu.Unlock()
}

// EffectiveWebPolicy returns AM_WEB_POLICY if set, else the configured policy.
func EffectiveWebPolicy() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AM_WEB_POLICY"))); v != "" {
		switch v {
		case WebPolicyPrefer, WebPolicyForce, WebPolicyLastResort:
			return v
		}
	}
	webPolicyMu.RLock()
	defer webPolicyMu.RUnlock()
	return webPolicy
}

func skipTextOnly(a types.ProviderAdapter, req *types.ChatRequest, nativeAvailable bool) bool {
	if req == nil || len(req.Tools) == 0 || adapterSupportsTools(a) {
		return false
	}
	switch EffectiveWebPolicy() {
	case WebPolicyForce, WebPolicyPrefer:
		return false
	default:
		return nativeAvailable
	}
}

func shouldSkipAdapter(a types.ProviderAdapter, req *types.ChatRequest, nativeAvailable, strongerThanFree bool) bool {
	return skipTextOnly(a, req, nativeAvailable) || skipFreeWebHardTask(a, req, strongerThanFree)
}

type planAware interface {
	Plan() string
}

func adapterPlan(a types.ProviderAdapter) string {
	if p, ok := a.(planAware); ok {
		return strings.ToLower(strings.TrimSpace(p.Plan()))
	}
	return ""
}

// isFreeWebAdapter is true for web-session adapters on an explicit free plan
// (or id containing "free"). Unknown/empty plan is treated as usable.
func isFreeWebAdapter(a types.ProviderAdapter) bool {
	grp := DetermineAdapterGroup(a)
	switch grp {
	case GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb:
		plan := adapterPlan(a)
		if plan == "free" {
			return true
		}
		return strings.Contains(strings.ToLower(a.ID()), "free")
	default:
		return false
	}
}

// skipFreeWebHardTask keeps free web off coding/fix when a stronger account exists.
func skipFreeWebHardTask(a types.ProviderAdapter, req *types.ChatRequest, strongerAvailable bool) bool {
	if !strongerAvailable || req == nil || !isFreeWebAdapter(a) {
		return false
	}
	switch req.TaskKind {
	case TaskCoding, TaskFix:
		return true
	default:
		return false
	}
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, types.ErrRateLimitReached) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "429") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "tpm") ||
		strings.Contains(s, "quota exceeded")
}

func isAdapterEligibleForRequest(a types.ProviderAdapter, req *types.ChatRequest) bool {
	if req == nil {
		return true
	}
	ide := IDEFromClientDialect(req.ClientDialect)
	grp := DetermineAdapterGroup(a)
	if ide == IDEClaude && IsClaudeSubscriptionGroup(grp) {
		return false
	}
	return true
}

func (r *AccountPoolRouter) hasStrongerThanFreeWeb(adapters []types.ProviderAdapter, req *types.ChatRequest) bool {
	return r.hasStrongerThanFreeWebExcluding(adapters, req, nil)
}

func (r *AccountPoolRouter) hasStrongerThanFreeWebExcluding(adapters []types.ProviderAdapter, req *types.ChatRequest, exclude map[string]bool) bool {
	native := r.usableToolBackendExcluding(adapters, req, exclude)
	for _, a := range adapters {
		if exclude != nil && exclude[a.ID()] {
			continue
		}
		if !isAdapterEligibleForRequest(a, req) {
			continue
		}
		if isFreeWebAdapter(a) {
			continue
		}
		if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
			continue
		}
		if r.cooling(a.ID()) {
			continue
		}
		if skipTextOnly(a, req, native) {
			continue
		}
		return true
	}
	return false
}

func (r *AccountPoolRouter) usableToolBackend(adapters []types.ProviderAdapter) bool {
	return r.usableToolBackendExcluding(adapters, nil, nil)
}

func (r *AccountPoolRouter) usableToolBackendExcluding(adapters []types.ProviderAdapter, req *types.ChatRequest, exclude map[string]bool) bool {
	for _, a := range adapters {
		if exclude != nil && exclude[a.ID()] {
			continue
		}
		if !isAdapterEligibleForRequest(a, req) {
			continue
		}
		if !adapterSupportsTools(a) {
			continue
		}
		if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
			continue
		}
		if r.cooling(a.ID()) {
			continue
		}
		return true
	}
	return false
}

// Send tries adapters in priority order.
//
// Routing precedence:
//  1. Session affinity pin (same tool-loop stays on one account)
//  2. Manual `am sw` preferred pin
//  3. New-session round-robin across living proxy layers (codex / AGY /
//     claude-web / chatgpt-web / gemini-web, then API) — never Claude sub
//  4. Group failover order (Claude IDE skips Claude subscription groups)
//
// Auto-switch leftovers update status preferred but do NOT steal new
// sessions (manualPin=false). SendNamed (X-Provider) always pins.
//
// Client tool loops send tools[]. Text-only backends are skipped when a
// usable native adapter exists; otherwise last-resort is web + MaybeWrapWebStream.
func (r *AccountPoolRouter) Send(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	applyTaskClassification(req)
	redactBeforeSend(req)

	r.mu.RLock()
	preferredID := r.preferred
	manual := r.manualPin
	adapters := make([]types.ProviderAdapter, len(r.adapters))
	copy(adapters, r.adapters)
	r.mu.RUnlock()

	sessionKey := guard.ExtractSessionKey(nil, req)
	nativeAvailable := r.usableToolBackendExcluding(adapters, req, nil)
	strongerThanFree := r.hasStrongerThanFreeWebExcluding(adapters, req, nil)

	// 1. Affinity wins for an in-flight session (unless account is dead or task transitions between web and coding).
	if sessionKey != "" {
		if pinned, ok := guard.GlobalAffinity().GetPinned(sessionKey); ok {
			if alive := r.adapterAlive(adapters, pinned, req, nativeAvailable, strongerThanFree); alive != nil {
				grp := DetermineAdapterGroup(alive)
				isWeb := IsWebGroup(grp)
				taskIsWeb := req != nil && IsWebTask(req.TaskKind)
				taskIsCoding := req != nil && (req.TaskKind == TaskCoding || req.TaskKind == TaskFix)

				if !r.manualPin && taskIsWeb && !isWeb && r.HasLivingGroup(GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb) {
					// Task switched to planning/clarify/review/analysis: route to web proxy to save coding limits
					preferredID = ""
					manual = false
				} else if !r.manualPin && taskIsCoding && isWeb && r.HasLivingGroup(GroupClaudeSub, GroupCodexSub, GroupCodexFree, GroupAGYSub, GroupAPIOther) {
					// Task switched to coding/fix: route to coding accounts
					preferredID = ""
					manual = false
				} else {
					preferredID = pinned
					manual = true // treat affinity as sticky for this request
				}
			} else {
				guard.GlobalAffinity().Unpin(sessionKey)
			}
		}
	}

	// 2. Manual `am sw` pin only — auto leftover preferred is ignored for new sessions.
	if !manual {
		preferredID = ""
	}

	// 3. New session WITH a session key: round-robin assign a living proxy
	// account and pin it. Anonymous requests (no session) keep group-order
	// failover below so existing priority behavior stays intact.
	if preferredID == "" && sessionKey != "" {
		if a := r.pickSessionAdapter(adapters, req, nativeAvailable, strongerThanFree); a != nil {
			preferredID = a.ID()
			guard.GlobalAffinity().Pin(sessionKey, preferredID)
		}
	}

	var errs []error
	var skippedPreferred bool

	if preferredID != "" {
		for _, a := range adapters {
			if a.ID() != preferredID {
				continue
			}
			if shouldSkipAdapter(a, req, nativeAvailable, strongerThanFree) {
				skippedPreferred = true
				errs = append(errs, fmt.Errorf("%s: skip weak/text-only backend for this task", a.ID()))
				break
			}
			if isQ, remaining, reason := guard.IsQuarantined(a.ID()); isQ {
				skippedPreferred = true
				errs = append(errs, fmt.Errorf("%s: quarantined (%s, remaining: %v)", a.ID(), reason, remaining.Round(time.Second)))
				break
			}
			if r.cooling(a.ID()) {
				skippedPreferred = true
				errs = append(errs, fmt.Errorf("%s: cooling down", a.ID()))
				break
			}
			isWeb := strings.Contains(a.ID(), "web") || strings.HasPrefix(a.ID(), "chatgpt")
			if err := guard.Pace(ctx, a.ID(), isWeb); err != nil {
				skippedPreferred = true
				errs = append(errs, fmt.Errorf("%s: %w", a.ID(), err))
				break
			}
			ch, err := a.SendMessageStream(ctx, req)
			if err == nil {
				guard.RecordSuccess(a.ID())
				if sessionKey != "" {
					guard.GlobalAffinity().Pin(sessionKey, a.ID())
				}
				// Preferred hit: keep manualPin as-is (am sw stays sticky).
				r.markUsed(a.ID(), false)
				return ch, nil
			}
			guard.RecordError(a.ID(), err)
			if sessionKey != "" {
				guard.GlobalAffinity().Unpin(sessionKey)
			}
			if errors.Is(err, types.ErrRateLimitReached) || isRateLimitError(err) {
				r.setCooldown(a.ID())
				skippedPreferred = true
				term.LogFailover("preferred %s rate-limited — failing over", a.ID())
				errs = append(errs, fmt.Errorf("%s: %w", a.ID(), err))
				break
			}
			log.Printf("router: preferred adapter %s failed (%v) — failing over", a.ID(), err)
			skippedPreferred = true
			term.LogWarn("preferred %s failed, next: %v", a.ID(), err)
			errs = append(errs, fmt.Errorf("%s: %w", a.ID(), err))
			break
		}
		if !skippedPreferred && len(errs) == 0 {
			log.Printf("router: preferred adapter %q not in pool — clearing preference", preferredID)
			r.ClearPreferred()
		}
	}

	failedInReq := make(map[string]bool)
	if preferredID != "" && skippedPreferred {
		failedInReq[preferredID] = true
	}

	// Partition adapters into priority groups
	groupBuckets := make(map[string][]types.ProviderAdapter)
	for _, a := range adapters {
		grp := DetermineAdapterGroup(a)
		groupBuckets[grp] = append(groupBuckets[grp], a)
	}

	for _, grpKey := range GroupOrderForRequest(req) {
		grpAdapters := groupBuckets[grpKey]
		if len(grpAdapters) == 0 {
			continue
		}

		r.mu.Lock()
		if r.groupIndices == nil {
			r.groupIndices = make(map[string]int)
		}
		startIdx := r.groupIndices[grpKey]
		r.mu.Unlock()

		for step := 0; step < len(grpAdapters); step++ {
			currIdx := (startIdx + step) % len(grpAdapters)
			a := grpAdapters[currIdx]

			if preferredID != "" && a.ID() == preferredID && !skippedPreferred {
				continue
			}
			curNative := r.usableToolBackendExcluding(adapters, req, failedInReq)
			curStronger := r.hasStrongerThanFreeWebExcluding(adapters, req, failedInReq)
			if shouldSkipAdapter(a, req, curNative, curStronger) {
				continue
			}
			if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
				continue
			}
			if r.cooling(a.ID()) {
				continue
			}
			isWeb := strings.Contains(a.ID(), "web") || strings.HasPrefix(a.ID(), "chatgpt")
			if err := guard.Pace(ctx, a.ID(), isWeb); err != nil {
				continue
			}
			ch, err := a.SendMessageStream(ctx, req)
			if err == nil {
				guard.RecordSuccess(a.ID())
				if sessionKey != "" {
					guard.GlobalAffinity().Pin(sessionKey, a.ID())
				}
				r.mu.Lock()
				r.groupIndices[grpKey] = (currIdx + 1) % len(grpAdapters)
				r.mu.Unlock()

				was := preferredID
				// Failover from a manual pin promotes status preferred but
				// clears manualPin so the next NEW session re-balances.
				r.markUsed(a.ID(), was != "" && manual)
				if was != "" && was != a.ID() {
					term.LogPool("auto-switch %s → %s [%s · %s]", was, a.ID(), GroupDisplayName(grpKey), RoleForGroup(grpKey))
				} else if was == "" {
					term.LogPool("active provider → %s [%s · %s]", a.ID(), GroupDisplayName(grpKey), RoleForGroup(grpKey))
				}
				return ch, nil
			}
			failedInReq[a.ID()] = true
			guard.RecordError(a.ID(), err)
			if errors.Is(err, types.ErrRateLimitReached) || isRateLimitError(err) {
				// Use upstream Retry-After hint when available (RateLimitError carries it).
				r.setCooldownAdaptive(a.ID(), types.ExtractRetryAfter(err))
			}
			term.LogWarn("%s failed (%s), next: %v", a.ID(), GroupDisplayName(grpKey), err)
			errs = append(errs, fmt.Errorf("%s: %w", a.ID(), err))
		}
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("router: no adapters configured, or all in cooldown")
	}
	return nil, errors.Join(errs...)
}

func (r *AccountPoolRouter) adapterAlive(adapters []types.ProviderAdapter, id string, req *types.ChatRequest, nativeAvailable, strongerThanFree bool) types.ProviderAdapter {
	for _, a := range adapters {
		if a.ID() != id {
			continue
		}
		if !isAdapterEligibleForRequest(a, req) {
			return nil
		}
		if shouldSkipAdapter(a, req, nativeAvailable, strongerThanFree) {
			return nil
		}
		if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
			return nil
		}
		if r.cooling(a.ID()) {
			return nil
		}
		return a
	}
	return nil
}

// pickSessionAdapter returns the next living proxy-layer adapter for a new
// session (round-robin). Nil when the pool is empty / all cooling.
func (r *AccountPoolRouter) pickSessionAdapter(adapters []types.ProviderAdapter, req *types.ChatRequest, nativeAvailable, strongerThanFree bool) types.ProviderAdapter {
	living := r.livingForSessionBalance(adapters, req, nativeAvailable, strongerThanFree)
	if len(living) == 0 {
		return nil
	}
	// If task is a web task, prioritize round-robin among living web proxy adapters
	if req != nil && IsWebTask(req.TaskKind) {
		var webLiving []types.ProviderAdapter
		for _, a := range living {
			if IsWebGroup(DetermineAdapterGroup(a)) {
				webLiving = append(webLiving, a)
			}
		}
		if len(webLiving) > 0 {
			r.mu.Lock()
			idx := r.sessionRR % len(webLiving)
			r.sessionRR++
			r.mu.Unlock()
			return webLiving[idx]
		}
	} else if req != nil && (req.TaskKind == TaskCoding || req.TaskKind == TaskFix) {
		var codeLiving []types.ProviderAdapter
		for _, a := range living {
			if !IsWebGroup(DetermineAdapterGroup(a)) {
				codeLiving = append(codeLiving, a)
			}
		}
		if len(codeLiving) > 0 {
			r.mu.Lock()
			idx := r.sessionRR % len(codeLiving)
			r.sessionRR++
			r.mu.Unlock()
			return codeLiving[idx]
		}
	}
	r.mu.Lock()
	idx := r.sessionRR % len(living)
	r.sessionRR++
	r.mu.Unlock()
	return living[idx]
}

func (r *AccountPoolRouter) cooling(id string) bool {
	if isQ, _, _ := guard.IsQuarantined(id); isQ {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	cd, exists := r.cooldownMap[id]
	if !exists {
		return false
	}
	return time.Now().Before(cd)
}

// setCooldown places adapter id into cooldown for the default rateLimitCooldown duration.
func (r *AccountPoolRouter) setCooldown(id string) {
	r.setCooldownDuration(id, rateLimitCooldown)
}

// setCooldownAdaptive uses the upstream Retry-After hint when available,
// falling back to rateLimitCooldown. The duration is clamped to
// [minAdaptiveCooldown, maxAdaptiveCooldown] so we never thrash on 1-second
// hints or block for hours on misconfigured responses.
func (r *AccountPoolRouter) setCooldownAdaptive(id string, retryAfter time.Duration) {
	if retryAfter <= 0 {
		r.setCooldownDuration(id, rateLimitCooldown)
		return
	}
	d := retryAfter
	if d < minAdaptiveCooldown {
		d = minAdaptiveCooldown
	}
	if d > maxAdaptiveCooldown {
		d = maxAdaptiveCooldown
	}
	r.setCooldownDuration(id, d)
}

func (r *AccountPoolRouter) setCooldownDuration(id string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cooldownMap[id] = time.Now().Add(d)
}

// markUsed records lastUsed; when promote is true also updates preferred
// for status display and CLEARS manualPin so auto leftovers do not steal
// the next new session (only `am sw` / SetPreferred sets manualPin).
func (r *AccountPoolRouter) markUsed(id string, promote bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUsed = id
	if promote {
		r.preferred = id
		r.manualPin = false
	}
}

// Status returns a summary map of each adapter, its priority, cooldown, and whether it is preferred.
func (r *AccountPoolRouter) Status() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]map[string]any, 0, len(r.adapters))
	for _, a := range r.adapters {
		cd := r.cooldownMap[a.ID()]
		cooling := time.Now().Before(cd)
		report := guard.GlobalHealth().GetReport(a.ID())
		grp := DetermineAdapterGroup(a)
		m := map[string]any{
			"id":            a.ID(),
			"priority":      a.Priority(),
			"group":         grp,
			"group_display": GroupDisplayName(grp),
			"role":          RoleForGroup(grp),
			"cooling":       cooling,
			"preferred":     a.ID() == r.preferred,
			"manual_pin":    a.ID() == r.preferred && r.manualPin,
			"last_used":     a.ID() == r.lastUsed,
			"in_pool":       true,
			"health_score":  report.Score,
			"health_status": report.Status,
		}
		if isQ, until, reason := guard.IsQuarantined(a.ID()); isQ {
			m["quarantined"] = true
			m["quarantine_reason"] = reason
			m["quarantine_remaining"] = until.Round(time.Second).String()
		}
		if cooling {
			m["cooldown_until"] = cd.Format(time.RFC3339)
		}
		res = append(res, m)
	}
	return res
}

// Reload updates the router's rotate-pool adapters and merges them into the
// addressable directory (clearing any stale or disabled adapters).
func (r *AccountPoolRouter) Reload(adapters []types.ProviderAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.groupIndices == nil {
		r.groupIndices = make(map[string]int)
	}
	sorted := make([]types.ProviderAdapter, len(adapters))
	copy(sorted, adapters)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority() < sorted[j].Priority() })
	r.adapters = sorted
	r.directory = make(map[string]types.ProviderAdapter, len(sorted))
	for _, a := range sorted {
		r.directory[a.ID()] = a
	}
	if r.preferred != "" {
		found := false
		for _, a := range sorted {
			if a.ID() == r.preferred {
				found = true
				break
			}
		}
		if !found {
			r.preferred = ""
			r.manualPin = false
		}
	}
}
