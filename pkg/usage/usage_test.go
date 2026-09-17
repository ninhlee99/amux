package usage

import (
	"os"
	"testing"
	"time"

	"amux-accounts/pkg/types"
)

func TestUsage_Commas(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{123, "123"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tc := range cases {
		got := Commas(tc.in)
		if got != tc.want {
			t.Errorf("Commas(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUsage_Labels(t *testing.T) {
	if got := ProjectLabel("/Users/foo/project-bar"); got != "project-bar" {
		t.Errorf("ProjectLabel mismatch: %q", got)
	}
	if got := ProjectLabel(""); got != "-" {
		t.Errorf("ProjectLabel empty mismatch: %q", got)
	}

	if got := SessionLabel("1234567890abcdef"); got != "12345678" {
		t.Errorf("SessionLabel mismatch: %q", got)
	}
	if got := SessionLabel(""); got != "-" {
		t.Errorf("SessionLabel empty mismatch: %q", got)
	}
}

func TestUsage_Aggregations(t *testing.T) {
	entries := []types.UsageEntry{
		{
			Time:    time.Now(),
			Account: "acc1",
			Model:   "claude-3-5",
			Project: "/path/to/myproj",
			Session: "sess-123456",
			Input:   100,
			Output:  200,
		},
		{
			Time:    time.Now(),
			Account: "acc1",
			Model:   "claude-3-5",
			Project: "/path/to/myproj",
			Session: "sess-123456",
			Input:   50,
			Output:  100,
		},
	}

	byDay := map[string]*usageAgg{}
	var totalIn, totalOut, totalReqs int
	for _, e := range entries {
		bump(byDay, e.Time.Local().Format("2006-01-02"), e)
		totalIn += e.Input
		totalOut += e.Output
		totalReqs++
	}

	if totalIn != 150 || totalOut != 300 || totalReqs != 2 {
		t.Errorf("agg mismatch: in=%d, out=%d, reqs=%d", totalIn, totalOut, totalReqs)
	}
}

func TestChannelLabel(t *testing.T) {
	cases := map[string]string{
		"":                  "-",
		"-":                 "-",
		"claude:web:ninhle": "web",
		"chatgpt:tungnt":    "web",
		"gemini:web:01":     "web",
		"codex:01":          "api",
		"agy:01":            "api",
		"openai:groq":       "api",
	}
	for in, want := range cases {
		if got := ChannelLabel(in); got != want {
			t.Errorf("ChannelLabel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestUsage_DayWeekMonthReports(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("AM_HOME")
	defer os.Setenv("AM_HOME", origHome)
	os.Setenv("AM_HOME", tmpDir)

	// Populate sample usage entries
	t1, _ := time.Parse("2006-01-02 15:04:05", "2026-09-15 10:00:00")
	t2, _ := time.Parse("2006-01-02 15:04:05", "2026-09-15 14:30:00")
	t3, _ := time.Parse("2006-01-02 15:04:05", "2026-09-16 09:15:00")
	t4, _ := time.Parse("2006-01-02 15:04:05", "2026-09-22 16:00:00")

	entries := []types.UsageEntry{
		{
			Time:          t1,
			Account:       "claude-sub-1",
			Endpoint:      "/v1/messages",
			Input:         1000,
			Output:        200,
			CacheRead:     5000,
			CacheCreation: 300,
		},
		{
			Time:          t2,
			Account:       "claude-web-1",
			Endpoint:      "/v1/messages",
			Input:         500,
			Output:        100,
			CacheRead:     0,
			CacheCreation: 0,
		},
		{
			Time:          t3,
			Account:       "claude-sub-1",
			Endpoint:      "/v1/messages",
			Input:         2000,
			Output:        400,
			CacheRead:     10000,
			CacheCreation: 600,
		},
		{
			Time:          t4,
			Account:       "claude-sub-vip",
			Endpoint:      "/v1/messages",
			Input:         3000,
			Output:        600,
			CacheRead:     15000,
			CacheCreation: 900,
		},
	}

	for _, e := range entries {
		AppendUsageEntry(e)
	}

	loaded := LoadUsageEntries(time.Time{})
	if len(loaded) != 4 {
		t.Fatalf("expected 4 loaded entries, got %d", len(loaded))
	}

	// Test 1: Day View
	// Should run without crashing and output expected columns
	PrintUsageReport([]string{"day", "2026-09-15"})
	PrintUsageReport([]string{"day", "2026-09-15", "claude-sub-1"})

	// Test 2: Week View
	// Should cover week containing 2026-09-15
	PrintUsageReport([]string{"week", "2026-09-15"})

	// Test 3: Month View
	// Should cover 2026-09
	PrintUsageReport([]string{"month", "2026-09"})
}

