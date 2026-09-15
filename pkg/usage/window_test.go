package usage

import "testing"

func TestFormatTokens(t *testing.T) {
	if FormatTokens(0) != "0" {
		t.Fatalf("0: %s", FormatTokens(0))
	}
	if FormatTokens(850) != "850" {
		t.Fatalf("850: %s", FormatTokens(850))
	}
	if FormatTokens(12300) != "12k" {
		t.Fatalf("12300: %s", FormatTokens(12300))
	}
	if FormatTokens(200000) != "200k" {
		t.Fatalf("200k: %s", FormatTokens(200000))
	}
	if FormatTokens(1_000_000) != "1M" {
		t.Fatalf("1M: %s", FormatTokens(1_000_000))
	}
}
