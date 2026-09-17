package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
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

func adapterModel(a types.ProviderAdapter) string {
	if a == nil {
		return ""
	}
	type modeler interface{ Model() string }
	if m, ok := a.(modeler); ok {
		return m.Model()
	}
	type targetModeler interface{ TargetModel() string }
	if tm, ok := a.(targetModeler); ok {
		return tm.TargetModel()
	}
	v := reflect.ValueOf(a)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		f := v.FieldByName("TargetModel")
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
		f = v.FieldByName("Model")
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}

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
	rateLimitCooldown   = 2 * time.Minute
	minAdaptiveCooldown = 5 * time.Second
	maxAdaptiveCooldown = 30 * time.Minute
)

// AccountPoolRouter dispatches a ChatRequest to the highest-priority
// adapter that isn't currently cooling down. A preferred adapter from
// `am sw <provider>` is tried first; rate-limit failover promotes the
// winner to preferred so status stays in sync. Non-rate-limit failures
// on a pin do not silently jump to another provider.
//
// Account selection is based ONLY on cost/quota tier (subscription > web >
// api_key, see tier.go) — never on which IDE is asking, and never on
// whether the request carries tool/function calls. Every account type has
// identical capability; the Universal Tool Engine (pkg/tools) is what lets
// web accounts serve tool calls just like native backends.
//
// directory holds every addressable adapter (including out-of-pool) for
// explicit X-Provider routing via SendNamed.
type AccountPoolRouter struct {
	adapters    []types.ProviderAdapter
	directory   map[string]types.ProviderAdapter
	preferred   string
	manualPin   bool // true only after `am sw` / SetPreferred — auto leftover must not steal new sessions
	lastUsed    string
	mu          sync.RWMutex
	cooldownMap map[string]time.Time
	tierIndices map[types.AccountType]int
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
		adapters:    sorted,
		directory:   dir,
		cooldownMap: make(map[string]time.Time),
		tierIndices: make(map[types.AccountType]int),
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
// This only tunes model behavior (thinking budget / tier hint) — it never
// affects which account is selected.
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
	if err := guard.Pace(ctx, id, AdapterAccountType(a) == types.AccountTypeWeb); err != nil {
		return nil, err
	}
	ch, err := a.SendMessageStream(ctx, req)
	if err != nil {
		guard.RecordError(id, err)
		return nil, err
	}
	req.ServingAccount = id
	req.ServingModel = adapterModel(a)
	req.ServingAPI = string(AdapterAccountType(a))
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

// HasLivingAccounts reports whether at least one pool adapter is configured
// and not currently cooling down or quarantined.
func (r *AccountPoolRouter) HasLivingAccounts() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.adapters {
		if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
			continue
		}
		if cd, exists := r.cooldownMap[a.ID()]; exists && time.Now().Before(cd) {
			continue
		}
		return true
	}
	return false
}

// ConversationResetter is implemented by web adapters that keep a
// server-side chat thread. Called on `am sw` so the next turn does not
// continue an unrelated conversation.
type ConversationResetter interface {
	ResetConversation()
}

// ScopeConversationResetter is implemented by adapters that support scoped thread resets.
type ScopeConversationResetter interface {
	ResetConversationForScope(scopeKey string)
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

// ResetConversationForScope clears server-side web threads for a specific project/session scope.
func (r *AccountPoolRouter) ResetConversationForScope(scopeKey string) {
	r.mu.RLock()
	adapters := append([]types.ProviderAdapter(nil), r.adapters...)
	r.mu.RUnlock()
	for _, a := range adapters {
		if rr, ok := a.(ScopeConversationResetter); ok {
			rr.ResetConversationForScope(scopeKey)
		} else if rr, ok := a.(ConversationResetter); ok {
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

// Send tries adapters in strict cost/quota priority order.
//
// Routing precedence:
//  1. Session affinity pin (same tool-loop stays on one account for continuity)
//  2. Manual `am sw` preferred pin
//  3. Tier order: subscription -> web -> api_key (see tier.go), round-robin
//     within each tier among adapters that are not cooling / quarantined.
//
// Selection never considers whether the request carries tool/function
// calls, and never biases toward the calling IDE's own provider — every
// account type has equal capability (see pkg/tools for the translation
// layer that gives web/subscription accounts native tool-call parity).
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

	// 1. Affinity wins for an in-flight session (unless the account died).
	if sessionKey != "" {
		if pinned, ok := guard.GlobalAffinity().GetPinned(sessionKey); ok {
			if alive := r.adapterAlive(adapters, pinned); alive != nil {
				preferredID = pinned
				manual = true // treat affinity as sticky for this request
			} else {
				guard.GlobalAffinity().Unpin(sessionKey)
			}
		}
	}

	// 2. Manual `am sw` pin only — auto leftover preferred is ignored for new sessions.
	if !manual {
		preferredID = ""
	}

	var errs []error
	var skippedPreferred bool

	if preferredID != "" {
		for _, a := range adapters {
			if a.ID() != preferredID {
				continue
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
			isWeb := AdapterAccountType(a) == types.AccountTypeWeb
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

	// Partition adapters into cost/quota tiers.
	tierBuckets := make(map[types.AccountType][]types.ProviderAdapter)
	for _, a := range adapters {
		t := AdapterAccountType(a)
		tierBuckets[t] = append(tierBuckets[t], a)
	}

	for _, tier := range TierOrder {
		tierAdapters := tierBuckets[tier]
		if len(tierAdapters) == 0 {
			continue
		}

		r.mu.Lock()
		if r.tierIndices == nil {
			r.tierIndices = make(map[types.AccountType]int)
		}
		startIdx := r.tierIndices[tier]
		r.mu.Unlock()

		for step := 0; step < len(tierAdapters); step++ {
			currIdx := (startIdx + step) % len(tierAdapters)
			a := tierAdapters[currIdx]

			// Never retry the adapter already attempted in the preferred
			// block above — whether it succeeded (we'd have returned
			// already) or failed (retrying here would double-count the
			// failure against its health score).
			if preferredID != "" && a.ID() == preferredID {
				continue
			}
			if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
				continue
			}
			if r.cooling(a.ID()) {
				continue
			}
			isWeb := tier == types.AccountTypeWeb
			if err := guard.Pace(ctx, a.ID(), isWeb); err != nil {
				continue
			}
			callReq := req
			if (skippedPreferred || len(failedInReq) > 0) && req != nil && len(req.Messages) > 4 {
				// Secondary / fallback adapter is a cold account: compact messages so it does not
				// pay massive uncached token creation fees and burn its 5h/7d rate limit.
				cloned := *req
				cloned.Messages = ctxshrink.CompactForAccountSwitchProject(req.Project(), req.Messages, 6)
				callReq = &cloned
			}
			ch, err := a.SendMessageStream(ctx, callReq)
			if err == nil {
				req.ServingAccount = a.ID()
				req.ServingModel = adapterModel(a)
				req.ServingAPI = string(tier)
				callReq.ServingAccount = a.ID()
				callReq.ServingModel = adapterModel(a)
				callReq.ServingAPI = string(tier)
				guard.RecordSuccess(a.ID())
				if sessionKey != "" {
					guard.GlobalAffinity().Pin(sessionKey, a.ID())
				}
				r.mu.Lock()
				r.tierIndices[tier] = (currIdx + 1) % len(tierAdapters)
				r.mu.Unlock()

				was := preferredID
				// Failover from a manual pin promotes status preferred but
				// clears manualPin so the next NEW session re-balances.
				r.markUsed(a.ID(), was != "" && manual)
				if was != "" && was != a.ID() {
					term.LogPool("auto-switch %s → %s [%s]", was, a.ID(), TierDisplayName(tier))
				} else if was == "" {
					term.LogPool("active provider → %s [%s]", a.ID(), TierDisplayName(tier))
				}
				return ch, nil
			}
			failedInReq[a.ID()] = true
			guard.RecordError(a.ID(), err)
			if errors.Is(err, types.ErrRateLimitReached) || isRateLimitError(err) {
				// Use upstream Retry-After hint when available (RateLimitError carries it).
				r.setCooldownAdaptive(a.ID(), types.ExtractRetryAfter(err))
			}
			term.LogWarn("%s failed (%s), next: %v", a.ID(), TierDisplayName(tier), err)
			errs = append(errs, fmt.Errorf("%s: %w", a.ID(), err))
		}
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("router: no adapters configured, or all in cooldown")
	}
	return nil, errors.Join(errs...)
}

func (r *AccountPoolRouter) adapterAlive(adapters []types.ProviderAdapter, id string) types.ProviderAdapter {
	for _, a := range adapters {
		if a.ID() != id {
			continue
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
		tier := AdapterAccountType(a)
		m := map[string]any{
			"id":            a.ID(),
			"priority":      a.Priority(),
			"type":          string(tier),
			"type_display":  TierDisplayName(tier),
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
	if r.tierIndices == nil {
		r.tierIndices = make(map[types.AccountType]int)
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
	if r.lastUsed != "" {
		found := false
		for _, a := range sorted {
			if a.ID() == r.lastUsed {
				found = true
				break
			}
		}
		if !found {
			r.lastUsed = ""
		}
	}
}
