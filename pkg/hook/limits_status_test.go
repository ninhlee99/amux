package hook

import (
	"strings"
	"testing"
)

func TestEnsureCodexStatusLine_InsertsWhenMissing(t *testing.T) {
	got, changed := ensureCodexStatusLine("model = \"gpt-5\"\n")
	if !changed {
		t.Fatal("expected insert")
	}
	if !strings.Contains(got, "used-tokens") || !strings.Contains(got, "five-hour-limit") {
		t.Fatalf("missing used-tokens/5h, got %q", got)
	}
}

func TestEnsureCodexStatusLine_UpgradesOldOurs(t *testing.T) {
	in := "[tui]\nstatus_line = [\"five-hour-limit\", \"weekly-limit\"]\n"
	got, changed := ensureCodexStatusLine(in)
	if !changed {
		t.Fatal("expected upgrade")
	}
	if !strings.Contains(got, "used-tokens") {
		t.Fatalf("expected used-tokens, got %q", got)
	}
}

func TestEnsureCodexStatusLine_SkipsCustom(t *testing.T) {
	in := "[tui]\nstatus_line = [\"model\", \"git-branch\"]\n"
	got, changed := ensureCodexStatusLine(in)
	if changed {
		t.Fatalf("must not overwrite custom, got %q", got)
	}
	if got != in {
		t.Fatalf("content changed: %q", got)
	}
}

func TestRemoveOurCodexStatusLine(t *testing.T) {
	in := "[tui]\nstatus_line = [\"used-tokens\", \"five-hour-limit\", \"weekly-limit\"]\nnotifications = true\n"
	got, changed := removeOurCodexStatusLine(in)
	if !changed {
		t.Fatal("expected removal")
	}
	if reCodexStatusLine.MatchString(got) {
		t.Fatalf("status_line still present: %q", got)
	}
	if !strings.Contains(got, "notifications = true") {
		t.Fatalf("must keep other tui keys, got %q", got)
	}
}

func TestRemoveOurCodexStatusLine_SkipsCustom(t *testing.T) {
	in := "[tui]\nstatus_line = [\"model\", \"git-branch\", \"five-hour-limit\", \"weekly-limit\"]\n"
	got, changed := removeOurCodexStatusLine(in)
	if changed {
		t.Fatal("must not delete a custom status_line that only happens to include 5h/7d")
	}
	if got != in {
		t.Fatalf("content changed: %q", got)
	}
}
