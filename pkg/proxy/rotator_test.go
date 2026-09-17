package proxy

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

// TestRotator_EmptyProfiles exercises the Rotator against a profile
// directory with nothing saved in it — the state of a fresh install, or a
// machine where all profiles were removed. NewRotator/Load must not touch
// the system keychain (ListProfiles is empty, so profile.LoadClaudeToken is
// never called), which is what makes this testable without real macOS
// Keychain access; a Rotator test that actually exercises the keychain path
// is out of scope here.
func TestRotator_EmptyProfiles(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())

	r := NewRotator("claude")

	if got := r.Active(); got != "" {
		t.Errorf("expected empty Active() with no profiles, got %q", got)
	}
	if got := r.Token(); got != "" {
		t.Errorf("expected empty Token() with no profiles, got %q", got)
	}
	if got := r.Names(); len(got) != 0 {
		t.Errorf("expected no profile names, got %v", got)
	}

	if err := r.ForceSwitch("nonexistent"); err == nil {
		t.Errorf("expected an error switching to a nonexistent profile")
	}

	status := r.Status()
	accts, ok := status["accounts"].([]map[string]any)
	if !ok {
		t.Fatalf("expected status[\"accounts\"] to be []map[string]any, got %T", status["accounts"])
	}
	if len(accts) != 0 {
		t.Errorf("expected 0 accounts in status, got %d", len(accts))
	}

	// Rotate/RefreshFromDisk/snapshotActiveIfChanged must be safe no-ops on
	// empty state rather than panicking on an out-of-range r.order[r.idx].
	r.Rotate("ghost-profile", "test")
	r.RefreshFromDisk()
	r.snapshotActiveIfChanged()
}

func TestParseUsedThreshold(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{95, 0.95},
		{0.95, 0.95},
		{90, 0.90},
		{0, DefaultUsedThreshold},
		{-1, DefaultUsedThreshold},
		{101, DefaultUsedThreshold},
		{1, 1},
	}
	for _, tt := range tests {
		got := ParseUsedThreshold(tt.in)
		if got != tt.want {
			t.Errorf("ParseUsedThreshold(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRotator_AllUnavailable(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())
	r := NewRotator("claude")
	if !r.AllUnavailable() {
		t.Fatal("empty rotator should be AllUnavailable")
	}
}

func TestRotator_ShouldFailoverToProviderPool(t *testing.T) {
	t.Setenv("AM_HOME", t.TempDir())

	// 0 profiles → false (no-token path handles empty)
	r0 := NewRotator("claude")
	if r0.ShouldFailoverToProviderPool() {
		t.Fatal("0 profiles: should not failover via ShouldFailover")
	}

	// 1 profile, available → false
	r1 := &Rotator{
		tool:          "claude",
		order:         []string{"solo"},
		tokens:        map[string]*types.Token{"solo": {Access: "tok"}},
		accounts:      map[string]string{},
		cooldown:      map[string]time.Time{},
		dead:          map[string]bool{},
		autoSwitches:  map[string]int{},
		manualSwitches: map[string]int{},
		usedThreshold: DefaultUsedThreshold,
	}
	if r1.ShouldFailoverToProviderPool() {
		t.Fatal("1 available profile: no failover")
	}

	// 1 profile, cooling → true
	r1.cooldown["solo"] = time.Now().Add(time.Hour)
	if !r1.ShouldFailoverToProviderPool() {
		t.Fatal("1 cooling profile: expect failover")
	}

	// 2 profiles, both cooling → true (exhaust Claude first, then pool)
	r2 := &Rotator{
		tool:   "claude",
		order:  []string{"a", "b"},
		tokens: map[string]*types.Token{"a": {Access: "t"}, "b": {Access: "t"}},
		accounts: map[string]string{},
		cooldown: map[string]time.Time{
			"a": time.Now().Add(time.Hour),
			"b": time.Now().Add(time.Hour),
		},
		dead:           map[string]bool{},
		autoSwitches:   map[string]int{},
		manualSwitches: map[string]int{},
		usedThreshold:  DefaultUsedThreshold,
	}
	if !r2.ShouldFailoverToProviderPool() {
		t.Fatal("all Claude cooling: expect provider-pool failover")
	}

	// 2 profiles, one resets → false + EnsureUsableActive switches back
	r2.cooldown["b"] = time.Time{}
	if r2.ShouldFailoverToProviderPool() {
		t.Fatal("one Claude available: no failover")
	}
	r2.idx = 0 // active is still "a" (cooling)
	if !r2.EnsureUsableActive() {
		t.Fatal("EnsureUsableActive should pick b")
	}
	if r2.Active() != "b" {
		t.Fatalf("active=%q, want b", r2.Active())
	}
}

// makeUsageResp builds a fake upstream *http.Response carrying a 5h-window
// utilization header, the same shape Rotator.Observe parses.
func makeUsageResp(statusCode int, used float64) *http.Response {
	h := http.Header{}
	if used >= 0 {
		h.Set("anthropic-ratelimit-unified-5h-utilization", strconv.FormatFloat(used, 'f', -1, 64))
	}
	return &http.Response{StatusCode: statusCode, Header: h}
}

// isCooling reports whether Status() shows a live cooldown for the profile.
func isCooling(t *testing.T, r *Rotator, name string) bool {
	t.Helper()
	status := r.Status()
	accts, _ := status["accounts"].([]map[string]any)
	for _, a := range accts {
		if a["profile"] == name {
			_, ok := a["cooldown_until"]
			return ok
		}
	}
	t.Fatalf("profile %q not found in status", name)
	return false
}

func newTestRotator(names ...string) *Rotator {
	tokens := map[string]*types.Token{}
	for _, n := range names {
		tokens[n] = &types.Token{Access: "tok"}
	}
	return &Rotator{
		tool:           "claude",
		order:          names,
		tokens:         tokens,
		accounts:       map[string]string{},
		cooldown:       map[string]time.Time{},
		dead:           map[string]bool{},
		autoSwitches:   map[string]int{},
		manualSwitches: map[string]int{},
		usedThreshold:  DefaultUsedThreshold,
	}
}

// TestRotator_SingleAccountReaches100PctThreshold locks in the single-pool
// rule: with exactly one subscription account and no pool alternatives, a
// preemptive near-limit signal (99% used, no hard 429) must NOT trigger a
// rotation — the lone account is allowed to ride all the way to 100%. A
// real 429 (the provider's own hard limit) must still fail it over,
// because at that point there is nothing left to preserve by waiting.
func TestRotator_SingleAccountReaches100PctThreshold(t *testing.T) {
	r := newTestRotator("solo")

	r.Observe(makeUsageResp(http.StatusOK, 0.99))
	if isCooling(t, r, "solo") {
		t.Fatal("single account with no alternatives must not preemptively cool down before a real 429")
	}

	r.Observe(makeUsageResp(http.StatusTooManyRequests, 1.0))
	if !isCooling(t, r, "solo") {
		t.Fatal("a real 429 must still cool down even the only account")
	}
}

// TestRotator_MultiAccountUsesConfiguredThreshold locks in the >=2 account
// rule: the configured threshold (default 95%) applies, so a 99%-used
// signal preemptively rotates away before ever hitting a hard 429.
func TestRotator_MultiAccountUsesConfiguredThreshold(t *testing.T) {
	r := newTestRotator("a", "b")

	r.Observe(makeUsageResp(http.StatusOK, 0.99))
	if !isCooling(t, r, "a") {
		t.Fatal("second account available: 99% used must preemptively rotate at the 95% threshold")
	}
}

// TestRotator_SingleAccountWithPoolAlternativesUsesThreshold verifies that
// "alternatives" isn't limited to other Claude subscriptions — a non-empty
// provider pool (Web/API accounts) also counts, so a solo Claude
// subscription with a living pool still uses the normal threshold instead
// of riding to 100%.
func TestRotator_SingleAccountWithPoolAlternativesUsesThreshold(t *testing.T) {
	r := newTestRotator("solo")
	r.SetPoolSize(1)

	r.Observe(makeUsageResp(http.StatusOK, 0.99))
	if !isCooling(t, r, "solo") {
		t.Fatal("solo account with a living provider pool must use the configured threshold, not ride to 100%")
	}
}
