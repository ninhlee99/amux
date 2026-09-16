package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/router"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
)

// proxyStatus is the decoded /_am/status payload.
type proxyStatus struct {
	Tool     string          `json:"tool"`
	Switches int             `json:"switches"`
	Sessions int             `json:"sessions"`
	Upstream string          `json:"upstream"`
	Mode     string          `json:"mode"`
	Accounts []statusAccount `json:"accounts"`
	Pool     []map[string]any `json:"pool"`
	ToolPool []map[string]any `json:"tool_pool"`
	Guard    map[string]any  `json:"guard,omitempty"`
}

type statusAccount struct {
	Profile        string   `json:"profile"`
	Account        string   `json:"account"`
	Active         bool     `json:"active"`
	Remaining      float64  `json:"remaining"`
	LimitReset     string   `json:"limit_reset"`
	Cooldown       string   `json:"cooldown_until"`
	Dead           bool     `json:"dead"`
	Disabled       bool     `json:"disabled"`
	FiveHUsed      *float64 `json:"5h_used"`
	FiveHReset     string   `json:"5h_reset"`
	SevenDUsed     *float64 `json:"7d_used"`
	SevenDReset    string   `json:"7d_reset"`
	AutoSwitches   int      `json:"auto_switches"`
	ManualSwitches int      `json:"manual_switches"`
}

func fetchProxyStatus() (*proxyStatus, error) {
	resp, err := http.Get(proxy.ProxyBase() + "/_am/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var s proxyStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// CmdStatus displays a modern realtime dashboard for proxy, Claude accounts,
// and the multi-provider pool.
func CmdStatus() {
	term.Header("amux status", "live gateway · accounts · pool by group")
	printStatusBody()
}

// printStatusBody renders full account/pool detail (Accounts tab + `amux status`).
func printStatusBody() {
	if !proxy.ProxyUp() {
		term.Section("proxy")
		term.KV("status", term.Badge("off", "down")+"  "+term.Dim("not running"))
		printAutoUpdate()
		term.PanelEnd()
		fmt.Println()
		profile.PrintLiveLogins()
		return
	}

	s, err := fetchProxyStatus()
	if err != nil {
		term.Error("query proxy status: %v", err)
		return
	}
	printProxyKV(s)
	printClaudeAccountsDetail(s)
	printPoolDetail(s)
	if s.Guard != nil {
		printGuardDetail(s.Guard)
	}
	fmt.Println()
}

func printProxyKV(s *proxyStatus) {
	modeLabel := s.Mode
	if modeLabel == "" {
		modeLabel = "claude"
	}
	term.Section("proxy")
	term.KV("status", term.Badge("ok", "up")+"  "+term.Bold(proxy.ProxyAddr()))
	term.KV("mode", term.Cyan(modeLabel))
	term.KV("traffic", fmt.Sprintf("%s  %s",
		term.White(fmt.Sprintf("%d tabs", s.Sessions)),
		term.Dim(fmt.Sprintf("%d switches", s.Switches)),
	))
	if proxy.IsPublic() {
		term.KV("bind", term.Yellow("0.0.0.0:"+proxyPort())+"  "+term.Badge("info", "pub"))
		term.KV("public", term.Cyan(proxy.FormatPublicHosts()))
	}
	printAutoUpdate()
	term.PanelEnd()
}

func proxyPort() string {
	_, port, err := splitHostPort(proxy.ProxyAddr())
	if err != nil || port == "" {
		return "8787"
	}
	return port
}

func splitHostPort(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}

func printClaudeAccountsDetail(s *proxyStatus) {
	if len(s.Accounts) == 0 {
		return
	}
	var rows []accountRow
	prints := map[string][]func(){}
	for i := range s.Accounts {
		a := s.Accounts[i]
		grp := router.ResolveProfileGroup("claude", claudeAccountPlan(a.Profile, a.Account))
		rows = append(rows, accountRow{Group: grp, ID: a.Profile})
		prints[grp] = append(prints[grp], func() {
			printOneClaudeAccount(s, a)
		})
	}
	forEachAccountGroup(rows, func(title, group string, _ []accountRow) {
		printDisplaySection(title)
		for _, fn := range prints[group] {
			fn()
		}
		term.PanelEnd()
	})
}

func claudeAccountPlan(profileName, account string) string {
	for _, p := range profile.ListProfiles("claude") {
		if (profileName != "" && (p.Name == profileName || p.ID == profileName)) ||
			(account != "" && p.Account == account) {
			return p.Plan
		}
	}
	return ""
}

func printOneClaudeAccount(s *proxyStatus, a statusAccount) {
	state := "idle"
	serving := false
	badge := term.Badge("idle", "idle")
	if a.Active {
		state = "active"
		serving = s.Mode != "provider" && !a.Dead && !a.Disabled
	}
	if a.Dead {
		state = "dead"
		serving = false
		badge = term.Badge("err", "dead")
	} else if a.Disabled {
		state = "off"
		serving = false
		badge = term.Badge("off", "off")
	} else if a.Cooldown != "" {
		if t, e := time.Parse(time.RFC3339, a.Cooldown); e == nil && time.Now().Before(t) {
			state = "cooldown"
			serving = false
			badge = term.Badge("warn", "cool")
		}
	}
	if serving {
		badge = term.Badge("ok", "live")
	} else if a.Active && state == "active" {
		badge = term.Badge("info", "pin")
	}

	acctName := a.Account
	if acctName == "" {
		acctName = a.Profile
	}
	extra := ""
	if a.Profile != "" && a.Account != "" && a.Profile != a.Account {
		extra = "  " + term.Dim("("+a.Profile+")")
	}
	term.Row(fmt.Sprintf("%s  %s%s", badge, term.Bold(acctName), extra))

	notes := []string{}
	if a.Cooldown != "" {
		if t, e := time.Parse(time.RFC3339, a.Cooldown); e == nil {
			notes = append(notes, "cooldown "+term.Yellow(t.Local().Format("15:04")))
		}
	}
	if a.Dead {
		notes = append(notes, term.Red("re-login needed"))
	}
	if a.Disabled {
		notes = append(notes, term.Dim("am on "+a.Profile))
	}
	if a.AutoSwitches+a.ManualSwitches > 0 {
		notes = append(notes, term.Dim(fmt.Sprintf("auto %d · manual %d", a.AutoSwitches, a.ManualSwitches)))
	}
	if len(notes) > 0 {
		term.Row("      " + strings.Join(notes, "  ·  "))
	}

	if a.FiveHUsed != nil {
		printLimitBar("5h", *a.FiveHUsed, a.FiveHReset)
	}
	if a.SevenDUsed != nil {
		printLimitBar("7d", *a.SevenDUsed, a.SevenDReset)
	}
	if a.FiveHUsed == nil && a.SevenDUsed == nil && a.Remaining >= 0 {
		used := 1 - a.Remaining
		if used < 0 {
			used = 0
		}
		printLimitBar("left", used, a.LimitReset)
	}
	_ = state
}

func printPoolDetail(s *proxyStatus) {
	if len(s.Pool) == 0 {
		return
	}

	var rows []accountRow
	byID := make(map[string]map[string]any, len(s.Pool))
	for _, p := range s.Pool {
		id, _ := p["id"].(string)
		grp, _ := p["group"].(string)
		if grp == "" {
			grp = router.ResolveAccountGroup(id, "", "", "")
		}
		prio, _ := p["priority"].(float64)
		rows = append(rows, withAPIProvider(accountRow{Group: grp, ID: id, Priority: int(prio)}))
		byID[id] = p
	}

	forEachAccountGroup(rows, func(title, _ string, members []accountRow) {
		printDisplaySection(title)
		for _, r := range members {
			printOnePoolAccount(s, byID[r.ID])
		}
		term.PanelEnd()
	})
}

func printOnePoolAccount(s *proxyStatus, p map[string]any) {
	if p == nil {
		return
	}
	id, _ := p["id"].(string)
	cooling, _ := p["cooling"].(bool)
	preferred, _ := p["preferred"].(bool)
	lastUsed, _ := p["last_used"].(bool)
	prio, _ := p["priority"].(float64)

	serving := false
	badge := term.Badge("idle", "idle")
	if preferred {
		badge = term.Badge("info", "pin")
	}
	if s.Mode == "provider" && lastUsed && !cooling {
		serving = true
	} else if preferred && s.Mode == "provider" && !cooling && !anyLastUsed(s.Pool) {
		serving = true
	}
	if cooling {
		badge = term.Badge("warn", "cool")
		serving = false
	}
	if serving {
		badge = term.Badge("ok", "live")
	}

	line := fmt.Sprintf("%s  %s  %s",
		badge,
		term.Dim(fmt.Sprintf("p%-2.0f", prio)),
		term.Bold(types.DisplayAccountID(id)),
	)
	if cooling {
		if cdUntil, ok := p["cooldown_until"].(string); ok && cdUntil != "" {
			if t, e := time.Parse(time.RFC3339, cdUntil); e == nil {
				line += "  " + term.Yellow("cooldown "+t.Local().Format("15:04"))
			}
		}
	}
	term.Row(line)
}

func printAutoUpdate() {
	if hook.IsAutoUpdateEnabled() {
		term.KV("update", term.Green("on")+"  "+term.Dim("every 6h"))
	} else {
		term.KV("update", term.Dim("off"))
	}
}

func printLimitBar(label string, used float64, resetISO string) {
	if used < 0 {
		used = 0
	}
	if used > 1 {
		used = 1
	}
	left := 1 - used
	term.Row(fmt.Sprintf("      %s  %s  %s used · %s left%s",
		term.Dim(fmt.Sprintf("%-4s", label)),
		term.RemainingBar(left, 14),
		term.Bold(fmt.Sprintf("%3.0f%%", used*100)),
		term.Bold(fmt.Sprintf("%3.0f%%", left*100)),
		term.Dim(resetSuffix(resetISO)),
	))
}

func anyLastUsed(pool []map[string]any) bool {
	for _, p := range pool {
		if last, _ := p["last_used"].(bool); last {
			return true
		}
	}
	return false
}

func resetSuffix(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	loc := time.Now().Location()
	t = t.In(loc)
	now := time.Now().In(loc)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "  resets " + t.Format("15:04")
	}
	if t.Sub(now) < 7*24*time.Hour {
		return "  resets " + t.Format("Mon 15:04")
	}
	return "  resets " + t.Format("Jan 02 15:04")
}

func printGuardDetail(g map[string]any) {
	if g == nil {
		return
	}
	term.Section("guard · account health & anti-ban")
	if pins, ok := g["active_pinned_sessions"].(float64); ok && pins > 0 {
		term.KV("sessions", fmt.Sprintf("%d sticky active", int(pins)))
	}
	accts, ok := g["accounts"].(map[string]any)
	if !ok || len(accts) == 0 {
		term.Row(term.Green("● All accounts 100/100 healthy") + "  " + term.Dim("no rate-limit or auth anomalies"))
		term.PanelEnd()
		return
	}

	ids := make([]string, 0, len(accts))
	for id := range accts {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		report, ok := accts[id].(map[string]any)
		if !ok {
			continue
		}
		score, _ := report["score"].(float64)
		status, _ := report["status"].(string)
		badge := term.Badge("ok", "healthy")
		switch status {
		case "degraded":
			badge = term.Badge("warn", "degraded")
		case "quarantined":
			badge = term.Badge("err", "quarantined")
		}
		scoreStr := fmt.Sprintf("%3.0f/100", score)
		reason := guardDeductionReason(report)
		if reason != "" {
			colorScore := term.Yellow(scoreStr)
			if score < 60 {
				colorScore = term.Red(scoreStr)
			}
			term.Row(fmt.Sprintf("%-20s  %s  %s  %s", term.Bold(id), badge, colorScore, term.Dim("("+reason+")")))
		} else {
			term.Row(fmt.Sprintf("%-20s  %s  %s", term.Bold(id), badge, term.White(scoreStr)))
		}
	}
	term.PanelEnd()
}

func guardDeductionReason(report map[string]any) string {
	if qReason, ok := report["quarantineReason"].(string); ok && qReason != "" {
		return qReason
	}
	score, _ := report["score"].(float64)
	if score >= 100 {
		return ""
	}
	errMsg, _ := report["lastErrorMessage"].(string)
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		if authErr, ok := report["consecutiveAuthErr"].(float64); ok && authErr > 0 {
			return "auth error"
		}
		return "transient error"
	}
	return cleanGuardErrorMessage(errMsg)
}

func cleanGuardErrorMessage(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "tpm") || strings.Contains(low, "tokens per minute"):
		return "status 413: TPM rate limit exceeded"
	case strings.Contains(low, "missing a thought_signature") || strings.Contains(low, "thought_signature"):
		return "status 400: missing thought_signature"
	case strings.Contains(low, "rate limit") || strings.Contains(low, "429"):
		return "429 rate limit reached"
	case strings.Contains(low, "request too large") || strings.Contains(low, "413"):
		return "status 413: request too large"
	case strings.Contains(low, "auth") || strings.Contains(low, "401") || strings.Contains(low, "403"):
		if strings.Contains(low, "401") {
			return "auth failure (401)"
		}
		if strings.Contains(low, "403") {
			return "auth failure (403 forbidden)"
		}
		return "auth failure"
	case strings.Contains(low, "deadline exceeded") || strings.Contains(low, "timeout"):
		return "request timeout"
	case strings.Contains(low, "status 500"):
		return "status 500: internal error"
	case strings.Contains(low, "status 502"):
		return "status 502: bad gateway"
	case strings.Contains(low, "status 503"):
		return "status 503: service unavailable"
	case strings.Contains(low, "status 404"):
		return "status 404: not found"
	case strings.Contains(low, "status 400"):
		return "status 400: invalid request"
	}

	// If it contains JSON with "message": "...", extract it
	if idx := strings.Index(msg, `"message":`); idx != -1 {
		rest := msg[idx+len(`"message":`):]
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, `"`) {
			rest = rest[1:]
			if end := strings.Index(rest, `"`); end != -1 {
				inner := rest[:end]
				return truncateRunes(inner, 45)
			}
		}
	}

	// Normalize single line, strip newlines
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.ReplaceAll(msg, "\t", " ")
	for strings.Contains(msg, "  ") {
		msg = strings.ReplaceAll(msg, "  ", " ")
	}
	msg = strings.TrimSpace(msg)

	return truncateRunes(msg, 45)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// CmdGuard displays anti-ban protection status or resets account health states.
func CmdGuard(args []string) {
	if len(args) > 0 && args[0] == "reset" {
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		if proxy.ProxyUp() {
			reqURL := proxy.ProxyBase() + "/_am/guard/reset"
			if target != "" {
				reqURL += "?target=" + target
			}
			_, _ = http.Get(reqURL)
		}
		if target == "" || target == "all" {
			guard.ResetAll()
			term.Success("guard: reset health scores and backoff state for all accounts")
		} else {
			guard.GlobalHealth().Reset(target)
			guard.GlobalPacer().Reset(target)
			term.Success("guard: reset health score for %s", target)
		}
		return
	}

	term.Header("amux guard", "account health · anti-ban · session affinity")
	if !proxy.ProxyUp() {
		reports := guard.GlobalHealth().GetAllReports()
		term.Section("guard (local state)")
		term.KV("status", term.Dim("proxy daemon not running"))
		if len(reports) == 0 {
			term.Row(term.Dim("No accounts tracked yet (run am proxy up to activate)"))
		} else {
			repIDs := make([]string, 0, len(reports))
			for id := range reports {
				repIDs = append(repIDs, id)
			}
			sort.Strings(repIDs)
			for _, id := range repIDs {
				rep := reports[id]
				reason := rep.QuarantineReason
				if reason == "" && rep.Score < 100 {
					reason = cleanGuardErrorMessage(rep.LastErrorMessage)
				}
				if reason != "" {
					term.Row(fmt.Sprintf("%-20s  score: %3d/100  status: %s  %s", id, rep.Score, rep.Status, term.Dim("("+reason+")")))
				} else {
					term.Row(fmt.Sprintf("%-20s  score: %3d/100  status: %s", id, rep.Score, rep.Status))
				}
			}
		}
		term.PanelEnd()
		fmt.Println()
		return
	}

	resp, err := http.Get(proxy.ProxyBase() + "/_am/guard")
	if err != nil {
		term.Error("failed to query guard status: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		term.Error("proxy returned HTTP %d (restart proxy with `am proxy restart` to activate latest features)", resp.StatusCode)
		return
	}
	var g map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		term.Error("invalid guard payload: %v", err)
		return
	}
	printGuardDetail(g)
	fmt.Println()
}
