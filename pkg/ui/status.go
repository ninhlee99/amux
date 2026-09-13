package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/proxy"
	"amux-accounts/pkg/term"
	"amux-accounts/pkg/types"
)

// proxyStatus is the decoded /_am/status payload.
type proxyStatus struct {
	Tool     string `json:"tool"`
	Switches int    `json:"switches"`
	Sessions int    `json:"sessions"`
	Upstream string `json:"upstream"`
	Mode     string `json:"mode"`
	Accounts []struct {
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
	} `json:"accounts"`
	Pool  []map[string]any `json:"pool"`
	Guard map[string]any   `json:"guard,omitempty"`
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
	term.Header("amux status", "live gateway · accounts · pool")
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
	term.Section("claude accounts")
	for _, a := range s.Accounts {
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
	term.PanelEnd()
}

func printPoolDetail(s *proxyStatus) {
	if len(s.Pool) == 0 {
		return
	}
	title := "provider pool"
	if s.Mode == "provider" {
		title = "provider pool  (active)"
	}
	term.Section(title)
	for _, p := range s.Pool {
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
	term.PanelEnd()
}

func printAutoUpdate() {
	if hook.IsAutoUpdateEnabled() {
		term.KV("update", term.Green("on")+"  "+term.Dim("every 6h"))
	} else {
		term.KV("update", term.Dim("off"))
	}
}

func printLimitBar(label string, used float64, resetISO string) {
	term.Row(fmt.Sprintf("      %s  %s  %s%s",
		term.Dim(fmt.Sprintf("%-4s", label)),
		term.ProgressBar(used, 14),
		term.Bold(fmt.Sprintf("%3.0f%%", used*100)),
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
	for id, val := range accts {
		report, ok := val.(map[string]any)
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
		reason, _ := report["quarantineReason"].(string)
		if reason != "" {
			term.Row(fmt.Sprintf("%-20s  %s  %s  %s", term.Bold(id), badge, term.Yellow(scoreStr), term.Dim(reason)))
		} else {
			term.Row(fmt.Sprintf("%-20s  %s  %s", term.Bold(id), badge, term.White(scoreStr)))
		}
	}
	term.PanelEnd()
}

// CmdGuard displays anti-ban protection status or resets account health states.
func CmdGuard(args []string) {
	if len(args) > 0 && args[0] == "reset" {
		target := ""
		if len(args) > 1 {
			target = args[1]
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
			for id, rep := range reports {
				term.Row(fmt.Sprintf("%-20s  score: %3d/100  status: %s", id, rep.Score, rep.Status))
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
