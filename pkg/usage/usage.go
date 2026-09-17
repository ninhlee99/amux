package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

func LoadUsageEntries(from time.Time) []types.UsageEntry {
	f, err := os.Open(UsageLogPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []types.UsageEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e types.UsageEntry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if !from.IsZero() && e.Time.Before(from) {
			continue
		}
		out = append(out, e)
	}
	return out
}

type usageAgg struct {
	in, out               int
	cacheRead, cacheWrite int
	reqs                  int
	endpoint              string
}

func (a *usageAgg) Total() int {
	return a.in + a.out + a.cacheRead + a.cacheWrite
}

// PrintUsageReport dispatches usage statistics according to the user specification:
// 1. day [YYYY-MM-DD] [account] - Account breakdown for a single day.
// 2. week [YYYY-MM-DD]           - Daily breakdown across all accounts for a week.
// 3. month [YYYY-MM]             - Weekly summary + daily breakdown for a month (max 1 month).
func PrintUsageReport(args []string) {
	if len(args) == 0 {
		printDayUsage(time.Now(), "")
		printUsageHelpNotice()
		return
	}

	mode := strings.ToLower(args[0])
	subArgs := args[1:]

	switch mode {
	case "day":
		targetDate := time.Now()
		accountFilter := ""
		if len(subArgs) > 0 {
			if parsed, err := time.Parse("2006-01-02", subArgs[0]); err == nil {
				targetDate = parsed
				if len(subArgs) > 1 {
					accountFilter = subArgs[1]
				}
			} else {
				// Maybe first arg is account name for today
				accountFilter = subArgs[0]
			}
		}
		printDayUsage(targetDate, accountFilter)

	case "week":
		targetDate := time.Now()
		if len(subArgs) > 0 {
			if parsed, err := time.Parse("2006-01-02", subArgs[0]); err == nil {
				targetDate = parsed
			} else {
				fmt.Fprintf(os.Stderr, "invalid date format %q (expected YYYY-MM-DD)\n", subArgs[0])
				return
			}
		}
		printWeekUsage(targetDate)

	case "month":
		targetDate := time.Now()
		if len(subArgs) > 0 {
			if parsed, err := time.Parse("2006-01", subArgs[0]); err == nil {
				targetDate = parsed
			} else if parsed, err := time.Parse("2006-01-02", subArgs[0]); err == nil {
				targetDate = parsed
			} else {
				fmt.Fprintf(os.Stderr, "invalid month format %q (expected YYYY-MM)\n", subArgs[0])
				return
			}
		}
		printMonthUsage(targetDate)

	case "help":
		printFullUsageHelp()

	default:
		// Attempt parsing as YYYY-MM-DD or YYYY-MM
		if parsed, err := time.Parse("2006-01-02", mode); err == nil {
			acct := ""
			if len(subArgs) > 0 {
				acct = subArgs[0]
			}
			printDayUsage(parsed, acct)
		} else if parsed, err := time.Parse("2006-01", mode); err == nil {
			printMonthUsage(parsed)
		} else {
			fmt.Fprintf(os.Stderr, "unknown usage command: %q\n\n", mode)
			printFullUsageHelp()
		}
	}
}

// ---------------- DAY VIEW ----------------

func printDayUsage(targetDate time.Time, accountFilter string) {
	loc := time.Local
	y, m, d := targetDate.Date()
	startOfDay := time.Date(y, m, d, 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1)

	entries := LoadUsageEntries(startOfDay)
	byAccount := make(map[string]*usageAgg)
	accountEndpoints := make(map[string]map[string]bool)

	var totalIn, totalOut, totalCacheRead, totalCacheWrite, totalReqs int

	for _, e := range entries {
		t := e.Time.In(loc)
		if t.Before(startOfDay) || !t.Before(endOfDay) {
			continue
		}
		acct := e.Account
		if acct == "" {
			acct = "unknown"
		}
		if accountFilter != "" && !strings.EqualFold(acct, accountFilter) {
			continue
		}

		bump(byAccount, acct, e)
		if accountEndpoints[acct] == nil {
			accountEndpoints[acct] = make(map[string]bool)
		}
		ep := e.Endpoint
		if ep == "" {
			ep = e.Model
		}
		if ep == "" {
			ep = "/v1/messages"
		}
		accountEndpoints[acct][ep] = true

		totalIn += e.Input
		totalOut += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheCreation
		totalReqs++
	}

	dateStr := startOfDay.Format("2006-01-02")
	fmt.Printf("\n======================= TOKEN USAGE: %s =======================\n", dateStr)
	if accountFilter != "" {
		fmt.Printf("Filter: Account = %s\n", accountFilter)
	}

	if len(byAccount) == 0 {
		fmt.Printf("No requests recorded for %s.\n\n", dateStr)
		return
	}

	fmt.Printf("%-20s %-18s %6s %10s %10s %10s %11s %11s\n",
		"ACCOUNT", "ENDPOINT", "REQS", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TOTAL")
	fmt.Printf("%-20s %-18s %6s %10s %10s %10s %11s %11s\n",
		"--------------------", "------------------", "------", "----------", "----------", "----------", "-----------", "-----------")

	accounts := make([]string, 0, len(byAccount))
	for a := range byAccount {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)

	for _, a := range accounts {
		agg := byAccount[a]
		eps := make([]string, 0, len(accountEndpoints[a]))
		for ep := range accountEndpoints[a] {
			eps = append(eps, ep)
		}
		sort.Strings(eps)
		epDisplay := strings.Join(eps, ",")
		if len(epDisplay) > 18 {
			epDisplay = epDisplay[:15] + "..."
		}

		fmt.Printf("%-20s %-18s %6d %10s %10s %10s %11s %11s\n",
			a,
			epDisplay,
			agg.reqs,
			Commas(agg.in),
			Commas(agg.out),
			Commas(agg.cacheRead),
			Commas(agg.cacheWrite),
			Commas(agg.Total()),
		)
	}

	totalSum := totalIn + totalOut + totalCacheRead + totalCacheWrite
	fmt.Printf("%-20s %-18s %6s %10s %10s %10s %11s %11s\n",
		"--------------------", "------------------", "------", "----------", "----------", "----------", "-----------", "-----------")
	fmt.Printf("%-20s %-18s %6d %10s %10s %10s %11s %11s\n\n",
		fmt.Sprintf("TOTAL (%d accounts)", len(accounts)),
		"-",
		totalReqs,
		Commas(totalIn),
		Commas(totalOut),
		Commas(totalCacheRead),
		Commas(totalCacheWrite),
		Commas(totalSum),
	)
}

// ---------------- WEEK VIEW ----------------

func printWeekUsage(targetDate time.Time) {
	loc := time.Local
	targetDate = targetDate.In(loc)

	// ISO week: Monday is 1, Sunday is 7
	weekday := int(targetDate.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day()-(weekday-1), 0, 0, 0, 0, loc)
	sunday := monday.AddDate(0, 0, 7)

	entries := LoadUsageEntries(monday)
	byDay := make(map[string]*usageAgg)

	var totalIn, totalOut, totalCacheRead, totalCacheWrite, totalReqs int

	for _, e := range entries {
		t := e.Time.In(loc)
		if t.Before(monday) || !t.Before(sunday) {
			continue
		}
		dayKey := t.Format("2006-01-02")
		bump(byDay, dayKey, e)

		totalIn += e.Input
		totalOut += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheCreation
		totalReqs++
	}

	_, weekNum := monday.ISOWeek()
	fmt.Printf("\n=================== TOKEN USAGE: WEEK %d (%s -> %s) ===================\n",
		weekNum, monday.Format("2006-01-02"), monday.AddDate(0, 0, 6).Format("2006-01-02"))

	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"DATE (DAY)", "REQS", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TOTAL")
	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"------------------", "------", "-----------", "-----------", "-----------", "-----------", "------------")

	for i := 0; i < 7; i++ {
		curDay := monday.AddDate(0, 0, i)
		dayKey := curDay.Format("2006-01-02")
		dayDisplay := curDay.Format("2006-01-02 (Mon)")
		switch curDay.Weekday() {
		case time.Monday:
			dayDisplay = curDay.Format("2006-01-02 (Mon)")
		case time.Tuesday:
			dayDisplay = curDay.Format("2006-01-02 (Tue)")
		case time.Wednesday:
			dayDisplay = curDay.Format("2006-01-02 (Wed)")
		case time.Thursday:
			dayDisplay = curDay.Format("2006-01-02 (Thu)")
		case time.Friday:
			dayDisplay = curDay.Format("2006-01-02 (Fri)")
		case time.Saturday:
			dayDisplay = curDay.Format("2006-01-02 (Sat)")
		case time.Sunday:
			dayDisplay = curDay.Format("2006-01-02 (Sun)")
		}

		agg := byDay[dayKey]
		if agg == nil {
			agg = &usageAgg{}
		}

		fmt.Printf("%-18s %6d %11s %11s %11s %11s %12s\n",
			dayDisplay,
			agg.reqs,
			Commas(agg.in),
			Commas(agg.out),
			Commas(agg.cacheRead),
			Commas(agg.cacheWrite),
			Commas(agg.Total()),
		)
	}

	totalSum := totalIn + totalOut + totalCacheRead + totalCacheWrite
	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"------------------", "------", "-----------", "-----------", "-----------", "-----------", "------------")
	fmt.Printf("%-18s %6d %11s %11s %11s %11s %12s\n\n",
		"WEEK TOTAL",
		totalReqs,
		Commas(totalIn),
		Commas(totalOut),
		Commas(totalCacheRead),
		Commas(totalCacheWrite),
		Commas(totalSum),
	)
}

// ---------------- MONTH VIEW ----------------

func printMonthUsage(targetDate time.Time) {
	loc := time.Local
	targetDate = targetDate.In(loc)

	startOfMonth := time.Date(targetDate.Year(), targetDate.Month(), 1, 0, 0, 0, 0, loc)
	endOfMonth := startOfMonth.AddDate(0, 1, 0)
	daysInMonth := int(endOfMonth.Sub(startOfMonth).Hours() / 24)

	entries := LoadUsageEntries(startOfMonth)

	byDay := make(map[int]*usageAgg)
	var totalIn, totalOut, totalCacheRead, totalCacheWrite, totalReqs int

	for _, e := range entries {
		t := e.Time.In(loc)
		if t.Before(startOfMonth) || !t.Before(endOfMonth) {
			continue
		}
		day := t.Day()
		agg := byDay[day]
		if agg == nil {
			agg = &usageAgg{}
			byDay[day] = agg
		}
		agg.in += e.Input
		agg.out += e.Output
		agg.cacheRead += e.CacheRead
		agg.cacheWrite += e.CacheCreation
		agg.reqs++

		totalIn += e.Input
		totalOut += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheCreation
		totalReqs++
	}

	monthStr := startOfMonth.Format("2006-01")
	fmt.Printf("\n====================== TOKEN USAGE: MONTH %s ======================\n", monthStr)

	// Part 1: Weekly Summary
	fmt.Printf("\n=== WEEKLY SUMMARY (%s) ===\n", monthStr)
	fmt.Printf("%-22s %6s %11s %11s %11s %11s %12s\n",
		"WEEK RANGE", "REQS", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TOTAL")
	fmt.Printf("%-22s %6s %11s %11s %11s %11s %12s\n",
		"----------------------", "------", "-----------", "-----------", "-----------", "-----------", "------------")

	type weekBucket struct {
		name     string
		startDay int
		endDay   int
		agg      usageAgg
	}

	weeks := []weekBucket{
		{name: fmt.Sprintf("Week 1 (01/%02d - 07/%02d)", startOfMonth.Month(), startOfMonth.Month()), startDay: 1, endDay: 7},
		{name: fmt.Sprintf("Week 2 (08/%02d - 14/%02d)", startOfMonth.Month(), startOfMonth.Month()), startDay: 8, endDay: 14},
		{name: fmt.Sprintf("Week 3 (15/%02d - 21/%02d)", startOfMonth.Month(), startOfMonth.Month()), startDay: 15, endDay: 21},
		{name: fmt.Sprintf("Week 4 (22/%02d - 28/%02d)", startOfMonth.Month(), startOfMonth.Month()), startDay: 22, endDay: 28},
	}
	if daysInMonth > 28 {
		weeks = append(weeks, weekBucket{
			name:     fmt.Sprintf("Week 5 (29/%02d - %02d/%02d)", startOfMonth.Month(), daysInMonth, startOfMonth.Month()),
			startDay: 29,
			endDay:   daysInMonth,
		})
	}

	for idx := range weeks {
		w := &weeks[idx]
		for d := w.startDay; d <= w.endDay && d <= daysInMonth; d++ {
			if a, ok := byDay[d]; ok {
				w.agg.in += a.in
				w.agg.out += a.out
				w.agg.cacheRead += a.cacheRead
				w.agg.cacheWrite += a.cacheWrite
				w.agg.reqs += a.reqs
			}
		}

		fmt.Printf("%-22s %6d %11s %11s %11s %11s %12s\n",
			w.name,
			w.agg.reqs,
			Commas(w.agg.in),
			Commas(w.agg.out),
			Commas(w.agg.cacheRead),
			Commas(w.agg.cacheWrite),
			Commas(w.agg.Total()),
		)
	}

	// Part 2: Daily Breakdown
	fmt.Printf("\n=== DAILY BREAKDOWN (%s) ===\n", monthStr)
	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"DATE (DAY)", "REQS", "INPUT", "OUTPUT", "CACHE READ", "CACHE WRITE", "TOTAL")
	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"------------------", "------", "-----------", "-----------", "-----------", "-----------", "------------")

	for d := 1; d <= daysInMonth; d++ {
		curDay := time.Date(targetDate.Year(), targetDate.Month(), d, 0, 0, 0, 0, loc)
		dayDisplay := curDay.Format("2006-01-02 (Mon)")
		switch curDay.Weekday() {
		case time.Monday:
			dayDisplay = curDay.Format("2006-01-02 (Mon)")
		case time.Tuesday:
			dayDisplay = curDay.Format("2006-01-02 (Tue)")
		case time.Wednesday:
			dayDisplay = curDay.Format("2006-01-02 (Wed)")
		case time.Thursday:
			dayDisplay = curDay.Format("2006-01-02 (Thu)")
		case time.Friday:
			dayDisplay = curDay.Format("2006-01-02 (Fri)")
		case time.Saturday:
			dayDisplay = curDay.Format("2006-01-02 (Sat)")
		case time.Sunday:
			dayDisplay = curDay.Format("2006-01-02 (Sun)")
		}

		agg := byDay[d]
		if agg == nil {
			agg = &usageAgg{}
		}

		fmt.Printf("%-18s %6d %11s %11s %11s %11s %12s\n",
			dayDisplay,
			agg.reqs,
			Commas(agg.in),
			Commas(agg.out),
			Commas(agg.cacheRead),
			Commas(agg.cacheWrite),
			Commas(agg.Total()),
		)
	}

	totalSum := totalIn + totalOut + totalCacheRead + totalCacheWrite
	fmt.Printf("%-18s %6s %11s %11s %11s %11s %12s\n",
		"------------------", "------", "-----------", "-----------", "-----------", "-----------", "------------")
	fmt.Printf("%-18s %6d %11s %11s %11s %11s %12s\n\n",
		"MONTH TOTAL",
		totalReqs,
		Commas(totalIn),
		Commas(totalOut),
		Commas(totalCacheRead),
		Commas(totalCacheWrite),
		Commas(totalSum),
	)
}

func printUsageHelpNotice() {
	fmt.Println("Usage Commands:")
	fmt.Println("  amux usage day [YYYY-MM-DD] [account]   View account usage for a specific day")
	fmt.Println("  amux usage week [YYYY-MM-DD]            View daily breakdown for that week (all accounts)")
	fmt.Println("  amux usage month [YYYY-MM]              View weekly summary and daily breakdown for that month")
	fmt.Println()
}

func printFullUsageHelp() {
	fmt.Print(`amux usage - Token & Request Analytics

USAGE:
  amux usage day [YYYY-MM-DD] [account]
      Display request count, input, output, cache read, cache write, and total
      tokens grouped by account for a single day (default: today).

  amux usage week [YYYY-MM-DD]
      Display request count and all token dimensions broken down by day across
      all accounts for the week containing the specified date (default: current week).

  amux usage month [YYYY-MM]
      Display weekly summary and daily breakdown across all accounts for the
      specified month (up to 1 month window; default: current month).

EXAMPLES:
  amux usage day                     # Today's token usage per account
  amux usage day 2026-09-17          # Past date token usage per account
  amux usage day 2026-09-17 claude-1 # Specific account on that date
  amux usage week                    # Current week daily breakdown
  amux usage week 2026-09-15         # Week containing Sep 15, 2026
  amux usage month                   # Current month (weekly summary + daily breakdown)
  amux usage month 2026-09           # Sep 2026 month analytics
`)
}

func formatCacheHit(cacheRead, uncachedIn int) string {
	total := cacheRead + uncachedIn
	if total == 0 {
		return "0%"
	}
	pct := (cacheRead * 100) / total
	return fmt.Sprintf("%d%%", pct)
}

func ProjectLabel(dir string) string {
	if dir == "" {
		return "-"
	}
	return filepath.Base(dir)
}

func ChannelLabel(account string) string {
	a := strings.ToLower(account)
	switch {
	case a == "" || a == "-":
		return "-"
	case strings.Contains(a, ":web") || strings.Contains(a, "_web") ||
		strings.HasPrefix(a, "chatgpt:"):
		return "web"
	default:
		return "api"
	}
}

func SessionLabel(id string) string {
	if id == "" {
		return "-"
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func bump(m map[string]*usageAgg, key string, e types.UsageEntry) {
	a := m[key]
	if a == nil {
		a = &usageAgg{}
		m[key] = a
	}
	a.in += e.Input
	a.out += e.Output
	a.cacheRead += e.CacheRead
	a.cacheWrite += e.CacheCreation
	a.reqs++
}

func Commas(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}
