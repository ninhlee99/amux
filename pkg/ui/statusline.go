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

	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/usage"
)

// StatuslineInput is JSON Claude Code / AGY pipe to a statusLine command.
type StatuslineInput struct {
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
// omits rate_limits / quota (API-key mode).
type LimitWindows struct {
	FiveHUsed  *float64
	SevenDUsed *float64
}

type limitSeg struct {
	Label string  // e.g. "5h", "Gemini 7d", "Claude 7d"
	Used  float64 // 0–1 utilization
}

// CmdStatusline renders Codex-style session tokens + rate/quota bars.
// Forces ANSI color (AGY/Claude pipe stdout is not a TTY). Optionally
// appends caveman badge when that hook script is present.
func CmdStatusline() {
	term.ForceColor()
	in, _ := ReadStatuslineInput(os.Stdin)
	line := RenderStatusline(in, fetchProxyLimits())
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
//	12k  ·  5h ████████ 100%  ·  Gemini 7d ███████░ 94%  ·  Claude 7d ███░░░░░ 40%
//
// No context-window size. Colors/bars assume term.ForceColor for pipes.
func RenderStatusline(in StatuslineInput, extra LimitWindows) string {
	var parts []string
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

func sessionTokens(in StatuslineInput) (int, bool) {
	cw := in.ContextWindow
	if cw == nil {
		return 0, false
	}
	if cu := cw.CurrentUsage; cu != nil {
		n := cu.InputTokens + cu.OutputTokens + cu.CacheCreationInputTokens + cu.CacheReadInputTokens
		if n < 0 {
			n = 0
		}
		return n, true
	}
	n := 0
	if cw.TotalInputTokens != nil {
		n += *cw.TotalInputTokens
	}
	if cw.TotalOutputTokens != nil {
		n += *cw.TotalOutputTokens
	}
	if n < 0 {
		n = 0
	}
	return n, true
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
	if len(out) > 0 {
		return out
	}
	if extra.FiveHUsed != nil {
		out = append(out, limitSeg{Label: "5h", Used: clamp01(*extra.FiveHUsed)})
	}
	if extra.SevenDUsed != nil {
		out = append(out, limitSeg{Label: "7d", Used: clamp01(*extra.SevenDUsed)})
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

func fetchProxyLimits() LimitWindows {
	if !proxy.ProxyUp() {
		return LimitWindows{}
	}
	client := &http.Client{Timeout: 150 * time.Millisecond}
	resp, err := client.Get(proxy.ProxyBase() + "/_am/status")
	if err != nil {
		return LimitWindows{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LimitWindows{}
	}
	var s proxyStatus
	if json.NewDecoder(resp.Body).Decode(&s) != nil {
		return LimitWindows{}
	}
	for _, a := range s.Accounts {
		if !a.Active || a.Dead || a.Disabled {
			continue
		}
		return LimitWindows{FiveHUsed: a.FiveHUsed, SevenDUsed: a.SevenDUsed}
	}
	return LimitWindows{}
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
