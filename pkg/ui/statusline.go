package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// CmdStatusline renders session tokens + 5h / 7d remaining.
func CmdStatusline() {
	in, _ := ReadStatuslineInput(os.Stdin)
	fmt.Println(RenderStatusline(in, fetchProxyLimits()))
}

func ReadStatuslineInput(r io.Reader) (StatuslineInput, error) {
	var in StatuslineInput
	err := json.NewDecoder(r).Decode(&in)
	return in, err
}

// RenderStatusline: `10k  5h [bar] N%left  7d [bar] N%left` — no window size.
func RenderStatusline(in StatuslineInput, extra LimitWindows) string {
	var b strings.Builder
	if used, ok := sessionTokens(in); ok {
		b.WriteString(usage.FormatTokens(used))
	}
	five, seven := mergeLimits(in, extra)
	writeLimit(&b, "5h", five)
	writeLimit(&b, "7d", seven)
	return b.String()
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

func writeLimit(b *strings.Builder, label string, used *float64) {
	if used == nil {
		return
	}
	if b.Len() > 0 {
		b.WriteString("  ")
	}
	left := 1 - *used
	if left < 0 {
		left = 0
	}
	if left > 1 {
		left = 1
	}
	b.WriteString(label)
	b.WriteByte(' ')
	b.WriteString(term.ProgressBar(left, 6))
	b.WriteString(fmt.Sprintf(" %d%%left", int(left*100+0.5)))
}

func mergeLimits(in StatuslineInput, extra LimitWindows) (fiveH, sevenD *float64) {
	if in.RateLimits != nil {
		if in.RateLimits.FiveHour != nil {
			v := in.RateLimits.FiveHour.UsedPercentage / 100
			fiveH = &v
		}
		if in.RateLimits.SevenDay != nil {
			v := in.RateLimits.SevenDay.UsedPercentage / 100
			sevenD = &v
		}
	}
	q5, q7 := limitsFromQuota(in.Quota)
	if fiveH == nil {
		fiveH = q5
	}
	if sevenD == nil {
		sevenD = q7
	}
	// AGY quota (and any other client-native buckets) must not inherit
	// Claude rotator 5h/7d from /_am/status — those windows belong to
	// another product.
	if len(in.Quota) > 0 {
		return fiveH, sevenD
	}
	if fiveH == nil {
		fiveH = extra.FiveHUsed
	}
	if sevenD == nil {
		sevenD = extra.SevenDUsed
	}
	return fiveH, sevenD
}

func limitsFromQuota(q map[string]statuslineQuota) (fiveH, sevenD *float64) {
	for k, v := range q {
		used := quotaUsed(v)
		if used == nil {
			continue
		}
		kl := strings.ToLower(k)
		switch {
		case strings.Contains(kl, "5h") || strings.Contains(kl, "five-hour") ||
			strings.Contains(kl, "five_hour") || strings.Contains(kl, "hourly"):
			if fiveH == nil {
				fiveH = used
			}
		case strings.Contains(kl, "week") || strings.Contains(kl, "7d") ||
			strings.Contains(kl, "seven"):
			if sevenD == nil {
				sevenD = used
			}
		}
	}
	return fiveH, sevenD
}

func quotaUsed(v statuslineQuota) *float64 {
	if v.UsedPercentage != nil {
		x := *v.UsedPercentage
		if x > 1 {
			x /= 100
		}
		return &x
	}
	if v.RemainingFraction != nil {
		x := 1 - *v.RemainingFraction
		if x < 0 {
			x = 0
		}
		return &x
	}
	return nil
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
