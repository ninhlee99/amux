package term

import (
	"strings"
	"testing"
)

func TestProgressBarBounds(t *testing.T) {
	Disable()
	defer Enable()
	if n := len([]rune(ProgressBar(0, 10))); n != 10 {
		t.Fatalf("empty bar len=%d got %q", n, ProgressBar(0, 10))
	}
	if n := len([]rune(ProgressBar(1, 10))); n != 10 {
		t.Fatalf("full bar len=%d", n)
	}
	if got := ProgressBar(1, 10); got != strings.Repeat("#", 10) {
		t.Fatalf("full want all # (no-color), got %q", got)
	}
	if got := ProgressBar(0, 10); got != strings.Repeat(".", 10) {
		t.Fatalf("empty want all ., got %q", got)
	}
	if n := len([]rune(ProgressBar(0.5, 10))); n != 10 {
		t.Fatalf("half bar len=%d", n)
	}
}

func TestRemainingBarBounds(t *testing.T) {
	Disable()
	defer Enable()
	if got := RemainingBar(1, 8); got != strings.Repeat("#", 8) {
		t.Fatalf("full remaining: %q", got)
	}
	if got := RemainingBar(0, 8); got != strings.Repeat(".", 8) {
		t.Fatalf("empty remaining: %q", got)
	}
	if n := len([]rune(RemainingBar(0.5, 8))); n != 8 {
		t.Fatalf("half len=%d", n)
	}
}

func TestBadgeNoColor(t *testing.T) {
	Disable()
	defer Enable()
	if got := Badge("active", "active"); got != "[ACTIVE]" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectThemeOverride(t *testing.T) {
	SetTheme("light")
	if cMuted == "\x1b[38;5;246m" {
		t.Fatal("light theme should not keep dark muted")
	}
	SetTheme("dark")
	if cAccent != "\x1b[38;5;80m" {
		t.Fatalf("dark accent = %q", cAccent)
	}
}

func TestVisibleFormatting(t *testing.T) {
	Disable()
	defer Enable()
	s := padVisible("hi", 10)
	if visibleLen(s) != 10 {
		t.Fatalf("pad visibleLen=%d", visibleLen(s))
	}
	colored := "\x1b[36mhello\x1b[0m"
	if visibleLen(colored) != 5 {
		t.Fatalf("ansi visibleLen=%d", visibleLen(colored))
	}
}
