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
