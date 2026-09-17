package ui

import (
	"strings"
	"testing"

	"amux-accounts/pkg/term"
)

func TestRenderStatusline_SessionTokensNoWindow(t *testing.T) {
	term.Disable()
	in := StatuslineInput{
		ContextWindow: &statuslineContext{
			CurrentUsage: &struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			}{InputTokens: 8000, CacheReadInputTokens: 2000},
		},
	}
	line := RenderStatusline(in, LimitWindows{})
	if !strings.Contains(line, "10k") || !strings.Contains(line, "tok") {
		t.Fatalf("expected session 10k tok, got %q", line)
	}
	if strings.Contains(line, "/") || strings.Contains(line, "128k") || strings.Contains(line, "200k") || strings.Contains(line, "1M") {
		t.Fatalf("must not show context window, got %q", line)
	}
}

func TestRenderStatusline_ClearShowsZero(t *testing.T) {
	term.Disable()
	in := StatuslineInput{ContextWindow: &statuslineContext{}}
	line := RenderStatusline(in, LimitWindows{})
	if !strings.Contains(line, "0") || !strings.Contains(line, "tok") {
		t.Fatalf("empty session should show 0 tok, got %q", line)
	}
}

func TestRenderStatusline_FiveHSevenDLeft(t *testing.T) {
	term.Disable()
	in := StatuslineInput{
		RateLimits: &statuslineRates{
			FiveHour: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 25},
			SevenDay: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 40},
		},
	}
	line := RenderStatusline(in, LimitWindows{})
	if !strings.Contains(line, "5h") || !strings.Contains(line, "7d") {
		t.Fatalf("missing 5h/7d, got %q", line)
	}
	if !strings.Contains(line, "75%") || !strings.Contains(line, "60%") {
		t.Fatalf("want remaining percents, got %q", line)
	}
	if strings.Contains(line, "%left") {
		t.Fatalf("old %%left format must go, got %q", line)
	}
	if !strings.Contains(line, "·") {
		t.Fatalf("want · separators like Codex, got %q", line)
	}
}

func TestRenderStatusline_AGYMultiQuota(t *testing.T) {
	term.Disable()
	gLeft, cLeft := 0.94, 0.40
	in := StatuslineInput{
		Quota: map[string]statuslineQuota{
			"gemini-weekly": {RemainingFraction: &gLeft},
			"claude-weekly": {RemainingFraction: &cLeft},
		},
	}
	line := RenderStatusline(in, LimitWindows{})
	if !strings.Contains(line, "Gemini 7d") || !strings.Contains(line, "Claude 7d") {
		t.Fatalf("AGY must show both Gemini + Claude quotas, got %q", line)
	}
	if !strings.Contains(line, "94%") || !strings.Contains(line, "40%") {
		t.Fatalf("want remaining percents, got %q", line)
	}
	if strings.Contains(line, "5h") {
		t.Fatalf("no 5h in quota payload, got %q", line)
	}
}

func TestRenderStatusline_AGYQuotaIgnoresClaudeRotatorExtra(t *testing.T) {
	term.Disable()
	left := 0.9
	five := 0.5
	in := StatuslineInput{
		Quota: map[string]statuslineQuota{
			"gemini-weekly": {RemainingFraction: &left},
		},
	}
	line := RenderStatusline(in, LimitWindows{FiveHUsed: &five})
	if strings.Contains(line, "5h") {
		t.Fatalf("AGY quota must not inherit Claude rotator 5h, got %q", line)
	}
	if !strings.Contains(line, "Gemini 7d") {
		t.Fatalf("AGY weekly quota label, got %q", line)
	}
}

func TestRenderStatusline_Empty(t *testing.T) {
	term.Disable()
	if got := RenderStatusline(StatuslineInput{}, LimitWindows{}); got != "" {
		t.Fatalf("no limits → empty line, got %q", got)
	}
}

func TestQuotaLabel(t *testing.T) {
	cases := map[string]string{
		"gemini-weekly": "Gemini 7d",
		"claude-weekly": "Claude 7d",
		"openai-5h":     "OpenAI 5h",
		"five_hour":     "5h",
	}
	for in, want := range cases {
		if got := quotaLabel(in); got != want {
			t.Fatalf("%s → %q want %q", in, got, want)
		}
	}
}

func TestReadStatuslineInput_Empty(t *testing.T) {
	in, err := ReadStatuslineInput(strings.NewReader(""))
	if err == nil && in.RateLimits != nil {
		t.Fatalf("empty stdin should be zero value")
	}
}

func TestRenderStatusline_AccountAndSessionTokens(t *testing.T) {
	term.Disable()
	inTok := 8500
	outTok := 1200
	in := StatuslineInput{
		Account: "ninhle",
		ContextWindow: &statuslineContext{
			TotalInputTokens:  &inTok,
			TotalOutputTokens: &outTok,
		},
	}
	line := RenderStatusline(in, LimitWindows{})
	if !strings.Contains(line, "claude:ninhle") {
		t.Fatalf("expected 'claude:ninhle', got %q", line)
	}
	if strings.Contains(line, "in ") || strings.Contains(line, "out ") {
		t.Fatalf("must not contain in/out breakdown, got %q", line)
	}
	if !strings.Contains(line, "9.7k tok") {
		t.Fatalf("expected '9.7k tok', got %q", line)
	}
}

func TestRenderStatusline_AccountFromExtra(t *testing.T) {
	term.Disable()
	in := StatuslineInput{
		ContextWindow: &statuslineContext{
			CurrentUsage: &struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			}{InputTokens: 500, OutputTokens: 100},
		},
	}
	extra := LimitWindows{Account: "openrouter:api:01"}
	line := RenderStatusline(in, extra)
	if !strings.Contains(line, "openrouter:api:01") {
		t.Fatalf("expected 'openrouter:api:01', got %q", line)
	}
	if strings.Contains(line, "in ") || strings.Contains(line, "out ") {
		t.Fatalf("must not contain in/out breakdown, got %q", line)
	}
	if !strings.Contains(line, "600 tok") {
		t.Fatalf("expected '600 tok', got %q", line)
	}
}

func TestFormatGroupAccount(t *testing.T) {
	cases := []struct {
		acct string
		tool string
		want string
	}{
		{"ninhle", "claude", "claude:ninhle"},
		{"claude:<ninhle>", "claude", "claude:ninhle"},
		{"gemini:api:01", "agy", "gemini:api:01"},
		{"codex:01", "codex", "codex:01"},
		{"chatgpt:ninhle21199", "chatgpt", "chatgpt:ninhle21199"},
		{"claude:code:01", "claude", "claude:code:01"},
	}
	for _, c := range cases {
		got := formatGroupAccount(c.acct, c.tool)
		if got != c.want {
			t.Errorf("formatGroupAccount(%q, %q) = %q, want %q", c.acct, c.tool, got, c.want)
		}
	}
}

func TestRenderStatusline_ClaudeAndCodexMatchAGY(t *testing.T) {
	term.Disable()
	defer term.Disable()
	inTok := 160000
	outTok := 7000

	extra := LimitWindows{
		Account: "gemini:api:01",
		ExtraSegs: []limitSeg{
			{Label: "Gemini 5h", Used: 0.32},
			{Label: "Gemini 7d", Used: 0.48},
		},
	}

	// 1. Claude Code
	claudeIn := StatuslineInput{
		Tool:    "claude",
		Account: "ninhle21199@gmail.com",
		ContextWindow: &statuslineContext{
			TotalInputTokens:  &inTok,
			TotalOutputTokens: &outTok,
		},
		RateLimits: &statuslineRates{
			FiveHour: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 0},
			SevenDay: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 100},
		},
	}

	claudeLine := RenderStatusline(claudeIn, extra)
	wantClaude := "gemini:api:01  ·  167k tok  ·  5h ######## 100%  ·  7d ........ 0%  ·  Gemini 5h #####... 68%  ·  Gemini 7d ####.... 52%"
	if claudeLine != wantClaude {
		t.Fatalf("Claude statusline mismatch:\n got:  %q\n want: %q", claudeLine, wantClaude)
	}

	// 2. Codex
	codexIn := StatuslineInput{
		Tool:    "codex",
		Account: "ninhle21199@gmail.com",
		ContextWindow: &statuslineContext{
			TotalInputTokens:  &inTok,
			TotalOutputTokens: &outTok,
		},
		RateLimits: &statuslineRates{
			FiveHour: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 0},
			SevenDay: &struct {
				UsedPercentage float64 `json:"used_percentage"`
			}{UsedPercentage: 100},
		},
	}

	codexLine := RenderStatusline(codexIn, extra)
	if codexLine != wantClaude {
		t.Fatalf("Codex statusline mismatch:\n got:  %q\n want: %q", codexLine, wantClaude)
	}

	// 3. Test with colors / Unicode enabled (standard terminal / AGY style)
	term.ForceColor()
	claudeUnicode := RenderStatusline(claudeIn, extra)
	codexUnicode := RenderStatusline(codexIn, extra)
	if claudeUnicode != codexUnicode {
		t.Fatalf("Claude and Codex unicode statusline must be identical:\n claude: %q\n codex:  %q", claudeUnicode, codexUnicode)
	}
	for _, want := range []string{
		"gemini:api:01",
		"167k",
		"tok",
		"5h",
		"████████",
		"100%",
		"7d",
		"░░░░░░░░",
		"0%",
		"Gemini 5h",
		"█████░░░",
		"68%",
		"Gemini 7d",
		"████░░░░",
		"52%",
	} {
		if !strings.Contains(claudeUnicode, want) {
			t.Fatalf("Unicode statusline missing expected component %q:\n got: %q", want, claudeUnicode)
		}
	}
}


