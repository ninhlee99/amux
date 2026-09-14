package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

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

// rateLimitCooldown is how long an adapter sits out after answering with a
// rate limit, before Send tries it again. Keep short: Claude/ChatGPT web
// free tiers often return brief 429s; a 30-minute sit-out made proxy requests
// unusable after one burst.
const rateLimitCooldown = 2 * time.Minute

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
func applyTaskClassification(req *types.ChatRequest) {
	if req == nil {
		return
	}
	c := ClassifyTask(req)
	if c.IsHeavy && req.TargetTier == "" {
		req.TargetTier = "pro"
		log.Printf("router: heavy task detected (%s) -> escalated to Pro tier", strings.Join(c.Reasons, ", "))
	}
	if c.NeedsThinking && !req.Thinking {
		req.Thinking = true
		if req.ReasoningEffort == "" {
			req.ReasoningEffort = c.ReasoningEffort
		}
		if req.ThinkingBudget <= 0 {
			req.ThinkingBudget = 2048
		}
		log.Printf("router: auto-enabled thinking mode (effort: %s, budget: %d, reasons: %s)",
			req.ReasoningEffort, req.ThinkingBudget, strings.Join(c.Reasons, ", "))
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
const skipWebWhenTools = true

func skipTextOnly(a types.ProviderAdapter, req *types.ChatRequest, nativeAvailable bool) bool {
	return skipWebWhenTools && nativeAvailable && req != nil && len(req.Tools) > 0 && !adapterSupportsTools(a)
}

func (r *AccountPoolRouter) usableToolBackend(adapters []types.ProviderAdapter) bool {
	for _, a := range adapters {
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
	nativeAvailable := r.usableToolBackend(adapters)

	// 1. Affinity wins for an in-flight session (unless account is dead).
	if sessionKey != "" {
		if pinned, ok := guard.GlobalAffinity().GetPinned(sessionKey); ok {
			if alive := r.adapterAlive(adapters, pinned, req, nativeAvailable); alive != nil {
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

	// 3. New session WITH a session key: round-robin assign a living proxy
	// account and pin it. Anonymous requests (no session) keep group-order
	// failover below so existing priority behavior stays intact.
	if preferredID == "" && sessionKey != "" {
		if a := r.pickSessionAdapter(adapters, req, nativeAvailable); a != nil {
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
			if skipTextOnly(a, req, nativeAvailable) {
				skippedPreferred = true
				errs = append(errs, fmt.Errorf("%s: skip text-only backend (client sent tools)", a.ID()))
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
			if errors.Is(err, types.ErrRateLimitReached) {
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

	// Partition adapters into priority groups
	groupBuckets := make(map[string][]types.ProviderAdapter)
	for _, a := range adapters {
		grp := DetermineAdapterGroup(a)
		groupBuckets[grp] = append(groupBuckets[grp], a)
	}

	ide := ""
	if req != nil {
		ide = IDEFromClientDialect(req.ClientDialect)
	}
	for _, grpKey := range ProxyGroupsForClient(ide) {
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

			if preferredID != "" && a.ID() == preferredID {
				continue
			}
			if skipTextOnly(a, req, nativeAvailable) {
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
			guard.RecordError(a.ID(), err)
			if errors.Is(err, types.ErrRateLimitReached) {
				r.setCooldown(a.ID())
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

func (r *AccountPoolRouter) adapterAlive(adapters []types.ProviderAdapter, id string, req *types.ChatRequest, nativeAvailable bool) types.ProviderAdapter {
	for _, a := range adapters {
		if a.ID() != id {
			continue
		}
		if skipTextOnly(a, req, nativeAvailable) {
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
func (r *AccountPoolRouter) pickSessionAdapter(adapters []types.ProviderAdapter, req *types.ChatRequest, nativeAvailable bool) types.ProviderAdapter {
	living := r.livingForSessionBalance(adapters, req, nativeAvailable)
	if len(living) == 0 {
		return nil
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

func (r *AccountPoolRouter) setCooldown(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cooldownMap[id] = time.Now().Add(rateLimitCooldown)
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
