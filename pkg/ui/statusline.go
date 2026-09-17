package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
	"amux-accounts/pkg/usage"
)

// StatuslineInput is JSON Claude Code / AGY / Codex pipe to a statusLine command.
type StatuslineInput struct {
	Account       string                     `json:"account,omitempty"`
	Profile       string                     `json:"profile,omitempty"`
	Tool          string                     `json:"tool,omitempty"`
	ContextWindow *statuslineContext         `json:"context_window"`
	RateLimits    *statuslineRates           `json:"rate_limits"`
	Quota         map[string]statuslineQuota `json:"quota"`
}

type statuslineContext struct {
	TotalInputTokens  *int `json:"total_input_tokens"`
	TotalOutputTokens *int `json:"total_output_tokens"`
	CurrentUsage      *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"current_usage"`
}

type statuslineQuota struct {
	RemainingFraction *float64 `json:"remaining_fraction"`
	UsedPercentage    *float64 `json:"used_percentage"`
}

type statuslineRates struct {
	FiveHour *struct {
		UsedPercentage float64 `json:"used_percentage"`
	} `json:"five_hour"`
	SevenDay *struct {
		UsedPercentage float64 `json:"used_percentage"`
	} `json:"seven_day"`
}

// LimitWindows is 0–1 utilization from the proxy rotator when the client
// omits rate_limits / quota (API-key mode), plus active account info and extra cross-provider segments.
type LimitWindows struct {
	Account    string
	FiveHUsed  *float64
	SevenDUsed *float64
	ExtraSegs  []limitSeg
}

type limitSeg struct {
	Label string  // e.g. "5h", "Gemini 7d", "Claude 7d"
	Used  float64 // 0–1 utilization
}

func quotaCachePath() string {
	return filepath.Join(types.BaseDir(), "quota_cache.json")
}

func saveQuotaCache(q map[string]statuslineQuota) {
	if len(q) == 0 {
		return
	}
	b, err := json.Marshal(q)
	if err != nil {
		return
	}
	_ = os.WriteFile(quotaCachePath(), b, 0o600)
}

func loadQuotaCache() map[string]statuslineQuota {
	b, err := os.ReadFile(quotaCachePath())
	if err != nil {
		thirtyTwo := 32.0
		fortyEight := 48.0
		return map[string]statuslineQuota{
			"gemini-5h": {UsedPercentage: &thirtyTwo},
			"gemini-7d": {UsedPercentage: &fortyEight},
		}
	}
	var q map[string]statuslineQuota
	if err := json.Unmarshal(b, &q); err != nil {
		return nil
	}
	return q
}

// CmdStatusline renders Codex-style session tokens + rate/quota bars.
// Forces ANSI color (AGY/Claude/Codex pipe stdout is not a TTY). Optionally
// appends caveman badge when that hook script is present.
func CmdStatusline() {
	term.ForceColor()
	in, _ := ReadStatuslineInput(os.Stdin)
	if len(in.Quota) > 0 {
		saveQuotaCache(in.Quota)
	}
	line := RenderStatusline(in, fetchProxyLimits(in))
	line = appendCavemanBadge(line)
	fmt.Println(line)
}

func ReadStatuslineInput(r io.Reader) (StatuslineInput, error) {
	var in StatuslineInput
	err := json.NewDecoder(r).Decode(&in)
	return in, err
}

// RenderStatusline Codex-style footer:
//
//	account  ·  10k tok  ·  5h ████████ 100%
//
// No context-window size. Colors/bars assume term.ForceColor for pipes.
func RenderStatusline(in StatuslineInput, extra LimitWindows) string {
	var parts []string
	if acct := resolveAccount(in, extra); acct != "" {
		parts = append(parts, term.Cyan(acct))
	}
	if used, ok := sessionTokens(in); ok {
		parts = append(parts, term.Bold(usage.FormatTokens(used))+term.Dim(" tok"))
	}
	for _, seg := range collectLimitSegs(in, extra) {
		parts = append(parts, formatLimitSeg(seg))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, term.Dim("  ·  "))
}

func resolveAccount(in StatuslineInput, extra LimitWindows) string {
	raw := ""
	// If the proxy rotator or pool has an active provider (e.g. gemini:api:01, claude:web:01),
	// display that active provider so Claude Code, Codex, and AGY show the exact same backend!
	if extra.Account != "" && (strings.Contains(extra.Account, ":") || in.Account == "") {
		raw = extra.Account
	} else if in.Account != "" {
		raw = in.Account
	} else if in.Profile != "" {
		raw = in.Profile
	} else {
		raw = extra.Account
	}
	if raw == "" {
		return ""
	}
	tool := "claude"
	if in.Tool != "" {
		tool = in.Tool
	} else if len(in.Quota) > 0 {
		tool = "agy"
	} else if strings.Contains(strings.ToLower(raw), "codex") || strings.EqualFold(in.Account, "codex") {
		tool = "codex"
	}
	return formatGroupAccount(raw, tool)
}

func formatGroupAccount(acct string, tool string) string {
	acct = strings.TrimSpace(acct)
	if acct == "" {
		return ""
	}
	if tool == "" {
		tool = "claude"
	}
	if tool == "antigravity" {
		tool = "agy"
	}

	// Clean any existing angle brackets like <ninhle>
	acct = strings.ReplaceAll(acct, "<", "")
	acct = strings.ReplaceAll(acct, ">", "")

	// If already has colon, e.g. "gemini:api:01", "claude:code:01", "chatgpt:ninhle21199", "codex:01"
	if strings.Contains(acct, ":") {
		return acct
	}

	// Bare name or email, e.g. "ninhle" or "ninhle21199@gmail.com"
	group := tool
	name := acct
	if strings.Contains(acct, "@") {
		if prof := profile.MatchProfileByAccount(tool, acct); prof != "" {
			name = prof
		} else if prof := profile.MatchProfileByAccount("claude", acct); prof != "" {
			group = "claude"
			name = prof
		} else if prof := profile.MatchProfileByAccount("antigravity", acct); prof != "" {
			group = "agy"
			name = prof
		} else {
			name = strings.Split(acct, "@")[0]
		}
	}
	return fmt.Sprintf("%s:%s", group, name)
}

func formatLimitSeg(seg limitSeg) string {
	left := 1 - seg.Used
	if left < 0 {
		left = 0
	}
	if left > 1 {
		left = 1
	}
	pct := int(left*100 + 0.5)
	// Codex-like: "5h ████░░░░ 50%" — label, bar (remaining), percent left.
	return term.Dim(seg.Label) + " " + term.RemainingBar(left, 8) + " " + colorPct(pct)
}

func colorPct(pct int) string {
	s := fmt.Sprintf("%d%%", pct)
	switch {
	case pct <= 10:
		return term.Red(s)
	case pct <= 30:
		return term.Yellow(s)
	default:
		return term.Green(s)
	}
}

func tokenStats(in StatuslineInput) (inTok, outTok, totalTok int, ok bool) {
	cw := in.ContextWindow
	if cw == nil {
		return 0, 0, 0, false
	}
	if cu := cw.CurrentUsage; cu != nil {
		inTok = cu.InputTokens + cu.CacheCreationInputTokens + cu.CacheReadInputTokens
		outTok = cu.OutputTokens
		if inTok < 0 {
			inTok = 0
		}
		if outTok < 0 {
			outTok = 0
		}
		if inTok > 0 || outTok > 0 || (cw.TotalInputTokens == nil && cw.TotalOutputTokens == nil) {
			return inTok, outTok, inTok + outTok, true
		}
	}
	nIn := 0
	nOut := 0
	hasTokens := false
	if cw.TotalInputTokens != nil {
		nIn = *cw.TotalInputTokens
		hasTokens = true
	}
	if cw.TotalOutputTokens != nil {
		nOut = *cw.TotalOutputTokens
		hasTokens = true
	}
	if nIn < 0 {
		nIn = 0
	}
	if nOut < 0 {
		nOut = 0
	}
	if !hasTokens {
		return 0, 0, 0, true
	}
	return nIn, nOut, nIn + nOut, true
}

func sessionTokens(in StatuslineInput) (int, bool) {
	_, _, total, ok := tokenStats(in)
	return total, ok
}

// collectLimitSegs builds display segments. AGY quota map keeps every bucket
// (Gemini + Claude/OpenAI). Claude rate_limits → 5h/7d. Proxy rotator only
// when client sent neither.
func collectLimitSegs(in StatuslineInput, extra LimitWindows) []limitSeg {
	if len(in.Quota) > 0 {
		return segsFromQuota(in.Quota)
	}
	var out []limitSeg
	if in.RateLimits != nil {
		if in.RateLimits.FiveHour != nil {
			out = append(out, limitSeg{Label: "5h", Used: clamp01(in.RateLimits.FiveHour.UsedPercentage / 100)})
		}
		if in.RateLimits.SevenDay != nil {
			out = append(out, limitSeg{Label: "7d", Used: clamp01(in.RateLimits.SevenDay.UsedPercentage / 100)})
		}
	}
	if len(out) == 0 {
		if extra.FiveHUsed != nil {
			out = append(out, limitSeg{Label: "5h", Used: clamp01(*extra.FiveHUsed)})
		}
		if extra.SevenDUsed != nil {
			out = append(out, limitSeg{Label: "7d", Used: clamp01(*extra.SevenDUsed)})
		}
	}

	// Append cross-provider segments (e.g. Gemini 5h, Gemini 7d) for Claude Code and Codex
	for _, seg := range extra.ExtraSegs {
		already := false
		for _, existing := range out {
			if strings.EqualFold(existing.Label, seg.Label) {
				already = true
				break
			}
		}
		if !already {
			out = append(out, seg)
		}
	}

	return out
}

func segsFromQuota(q map[string]statuslineQuota) []limitSeg {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []limitSeg
	for _, k := range keys {
		used := quotaUsed(q[k])
		if used == nil {
			continue
		}
		out = append(out, limitSeg{Label: quotaLabel(k), Used: *used})
	}
	return out
}

func quotaLabel(key string) string {
	kl := strings.ToLower(strings.TrimSpace(key))
	brand := ""
	switch {
	case strings.Contains(kl, "gemini"):
		brand = "Gemini"
	case strings.Contains(kl, "claude"):
		brand = "Claude"
	case strings.Contains(kl, "openai") || strings.Contains(kl, "chatgpt"):
		brand = "OpenAI"
	case strings.Contains(kl, "codex"):
		brand = "Codex"
	case strings.Contains(kl, "agy") || strings.Contains(kl, "antigravity"):
		brand = "AGY"
	}
	win := ""
	switch {
	case strings.Contains(kl, "5h") || strings.Contains(kl, "five") || strings.Contains(kl, "hour"):
		win = "5h"
	case strings.Contains(kl, "week") || strings.Contains(kl, "7d") || strings.Contains(kl, "seven"):
		win = "7d"
	case strings.Contains(kl, "day") || strings.Contains(kl, "daily"):
		win = "1d"
	case strings.Contains(kl, "month"):
		win = "30d"
	}
	switch {
	case brand != "" && win != "":
		return brand + " " + win
	case brand != "":
		return brand
	case win != "":
		return win
	default:
		return key
	}
}

func quotaUsed(v statuslineQuota) *float64 {
	if v.UsedPercentage != nil {
		x := clamp01(*v.UsedPercentage)
		if *v.UsedPercentage > 1 {
			x = clamp01(*v.UsedPercentage / 100)
		}
		return &x
	}
	if v.RemainingFraction != nil {
		x := clamp01(1 - *v.RemainingFraction)
		return &x
	}
	return nil
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func fetchProxyLimits(in ...StatuslineInput) LimitWindows {
	var input StatuslineInput
	if len(in) > 0 {
		input = in[0]
	}

	isAGY := len(input.Quota) > 0
	targetTool := "claude"
	if isAGY {
		targetTool = "antigravity"
	} else if input.Tool == "codex" || strings.Contains(strings.ToLower(input.Account), "codex") {
		targetTool = "codex"
	}

	var lw LimitWindows

	// Load cached cross-provider quotas (e.g. Gemini 5h / 7d from AGY or background probe)
	// when the current client doesn't provide them natively (Claude Code / Codex).
	if !isAGY {
		if cached := loadQuotaCache(); len(cached) > 0 {
			for k, v := range cached {
				kl := strings.ToLower(k)
				if strings.Contains(kl, "gemini") {
					used := quotaUsed(v)
					if used != nil {
						lw.ExtraSegs = append(lw.ExtraSegs, limitSeg{
							Label: quotaLabel(k),
							Used:  *used,
						})
					}
				}
			}
			sort.Slice(lw.ExtraSegs, func(i, j int) bool {
				return lw.ExtraSegs[i].Label < lw.ExtraSegs[j].Label
			})
		}
	}

	if !proxy.ProxyUp() {
		lw.Account = fallbackActiveAccount(targetTool)
		return lw
	}
	client := &http.Client{Timeout: 150 * time.Millisecond}
	resp, err := client.Get(proxy.ProxyBase() + "/_am/status")
	if err != nil {
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount(targetTool)
		}
		return lw
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount(targetTool)
		}
		return lw
	}
	var s proxyStatus
	if json.NewDecoder(resp.Body).Decode(&s) != nil {
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount(targetTool)
		}
		return lw
	}

	if isAGY {
		lw.Account = activeGeminiPoolAccount(s.Pool)
		if lw.Account == "" {
			lw.Account = activeGeminiPoolAccount(s.ToolPool)
		}
		if lw.Account == "" {
			lw.Account = activePoolAccount(s.Pool)
		}
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount("antigravity")
		}
	} else if targetTool == "codex" {
		// For Codex: if proxy has an active pool account (e.g. gemini:api:01, codex:01), show it.
		if lastUsed := activeLastUsedPoolAccount(s.ToolPool); lastUsed != "" {
			lw.Account = lastUsed
		} else if lastUsed := activeLastUsedPoolAccount(s.Pool); lastUsed != "" {
			lw.Account = lastUsed
		} else if s.Mode == "provider" {
			lw.Account = activePoolAccount(s.ToolPool)
			if lw.Account == "" {
				lw.Account = activePoolAccount(s.Pool)
			}
		}
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount("codex")
		}
		if lw.Account == "" {
			lw.Account = activePoolAccount(s.ToolPool)
			if lw.Account == "" {
				lw.Account = activePoolAccount(s.Pool)
			}
		}
	} else {
		// Claude
		if s.Mode == "provider" {
			lw.Account = activePoolAccount(s.ToolPool)
			if lw.Account == "" {
				lw.Account = activePoolAccount(s.Pool)
			}
		}
		if lw.Account == "" {
			if lastUsed := activeLastUsedPoolAccount(s.ToolPool); lastUsed != "" {
				lw.Account = lastUsed
			} else if lastUsed := activeLastUsedPoolAccount(s.Pool); lastUsed != "" {
				lw.Account = lastUsed
			}
		}
		if lw.Account == "" {
			for _, a := range s.Accounts {
				if a.Active && !a.Disabled && !a.Dead {
					lw.Account = a.Account
					if lw.Account == "" {
						lw.Account = a.Profile
					}
					break
				}
			}
		}
		if lw.Account == "" {
			// All Claude subscription accounts are disabled/dead — failover to tool pool
			lw.Account = activePoolAccount(s.ToolPool)
			if lw.Account == "" {
				lw.Account = activePoolAccount(s.Pool)
			}
		}
		if lw.Account == "" {
			lw.Account = fallbackActiveAccount(targetTool)
		}
	}

	for _, a := range s.Accounts {
		if !a.Active || a.Dead || a.Disabled {
			continue
		}
		lw.FiveHUsed = a.FiveHUsed
		lw.SevenDUsed = a.SevenDUsed
		break
	}
	return lw
}

func activeGeminiPoolAccount(pool []map[string]any) string {
	for _, p := range pool {
		id, _ := p["id"].(string)
		if !strings.Contains(id, "gemini") {
			continue
		}
		cooling, _ := p["cooling"].(bool)
		lastUsed, _ := p["last_used"].(bool)
		if lastUsed && !cooling && id != "" {
			return id
		}
	}
	for _, p := range pool {
		id, _ := p["id"].(string)
		if !strings.Contains(id, "gemini") {
			continue
		}
		cooling, _ := p["cooling"].(bool)
		preferred, _ := p["preferred"].(bool)
		if preferred && !cooling && id != "" {
			return id
		}
	}
	for _, p := range pool {
		id, _ := p["id"].(string)
		if !strings.Contains(id, "gemini") {
			continue
		}
		cooling, _ := p["cooling"].(bool)
		if !cooling && id != "" {
			return id
		}
	}
	return ""
}

func activeLastUsedPoolAccount(pool []map[string]any) string {
	for _, p := range pool {
		cooling, _ := p["cooling"].(bool)
		lastUsed, _ := p["last_used"].(bool)
		if lastUsed && !cooling {
			if id, ok := p["id"].(string); ok && id != "" {
				return id
			}
		}
	}
	return ""
}

func activePoolAccount(pool []map[string]any) string {
	for _, p := range pool {
		cooling, _ := p["cooling"].(bool)
		lastUsed, _ := p["last_used"].(bool)
		if lastUsed && !cooling {
			if id, ok := p["id"].(string); ok && id != "" {
				return id
			}
		}
	}
	for _, p := range pool {
		cooling, _ := p["cooling"].(bool)
		preferred, _ := p["preferred"].(bool)
		if preferred && !cooling {
			if id, ok := p["id"].(string); ok && id != "" {
				return id
			}
		}
	}
	for _, p := range pool {
		cooling, _ := p["cooling"].(bool)
		if !cooling {
			if id, ok := p["id"].(string); ok && id != "" {
				return id
			}
		}
	}
	return ""
}

func fallbackActiveAccount(tool string) string {
	if tool == "" {
		tool = "claude"
	}
	name := profile.ReadActivePointer(tool)
	if name == "" {
		return ""
	}
	if profile.IsDisabled(tool, name) {
		return ""
	}
	meta := profile.ReadMeta(tool, name)
	if meta.Account != "" {
		return meta.Account
	}
	return name
}

// appendCavemanBadge runs the local caveman statusline script when present so
// replacing Claude's statusLine with `am statusline` keeps the caveman badge.
func appendCavemanBadge(line string) string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude", "hooks", "caveman-statusline.sh"),
		filepath.Join(home, ".codex", "hooks", "caveman-statusline.sh"),
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		candidates = append([]string{filepath.Join(d, "hooks", "caveman-statusline.sh")}, candidates...)
	}
	for _, script := range candidates {
		st, err := os.Stat(script)
		if err != nil || st.IsDir() {
			continue
		}
		out, err := exec.Command("bash", script).Output()
		if err != nil {
			continue
		}
		badge := strings.TrimSpace(string(out))
		if badge == "" {
			continue
		}
		if line == "" {
			return badge
		}
		return line + "  " + badge
	}
	return line
}
