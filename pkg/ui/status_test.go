package ui

import (
	"strings"
	"testing"
)

func TestCleanGuardErrorMessage(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "gemini:api:01: status 400: Function call is missing a thought_signature in functionCall parts.",
			want:  "status 400: missing thought_signature",
		},
		{
			input: "groq:api:01: status 413: Request too large for model ... TPM: Limit 8000",
			want:  "status 413: TPM rate limit exceeded",
		},
		{
			input: "429 rate limit reached",
			want:  "429 rate limit reached",
		},
		{
			input: "auth failure: 401 Unauthorized",
			want:  "auth failure (401)",
		},
		{
			input: "context deadline exceeded",
			want:  "request timeout",
		},
	}

	for _, c := range cases {
		got := cleanGuardErrorMessage(c.input)
		if got != c.want {
			t.Errorf("cleanGuardErrorMessage(%q) = %q; want %q", c.input, got, c.want)
		}
	}
}

func TestGuardDeductionReason(t *testing.T) {
	// 100/100 should have no reason
	if r := guardDeductionReason(map[string]any{"score": float64(100)}); r != "" {
		t.Fatalf("expected empty reason for 100 score, got %q", r)
	}

	// Quarantine reason takes precedence
	rep := map[string]any{
		"score":            float64(30),
		"quarantineReason": "cooling down to prevent ban",
		"lastErrorMessage": "429 rate limit reached",
	}
	if r := guardDeductionReason(rep); r != "cooling down to prevent ban" {
		t.Fatalf("expected quarantine reason, got %q", r)
	}

	// Score deduction with error message
	rep2 := map[string]any{
		"score":            float64(90),
		"lastErrorMessage": "status 400: missing a thought_signature",
	}
	if r := guardDeductionReason(rep2); !strings.Contains(r, "missing thought_signature") {
		t.Fatalf("expected missing thought_signature, got %q", r)
	}
}
