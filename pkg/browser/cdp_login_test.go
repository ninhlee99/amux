package browser

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPickSessionCookieChunked(t *testing.T) {
	cookies := []cdpCookie{
		{Name: "__Secure-next-auth.session-token.0", Value: "AAA", Domain: ".chatgpt.com"},
		{Name: "__Secure-next-auth.session-token.1", Value: "BBB", Domain: ".chatgpt.com"},
		{Name: "oai-did", Value: "x", Domain: ".chatgpt.com"},
	}
	got := pickSessionCookie(cookies, ChatGPTWebLogin)
	want := "__Secure-next-auth.session-token.0=AAA; __Secure-next-auth.session-token.1=BBB"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPickSessionCookieExact(t *testing.T) {
	cookies := []cdpCookie{
		{Name: "sessionKey", Value: "sk-test", Domain: ".claude.ai"},
	}
	got := pickSessionCookie(cookies, ClaudeWebLogin)
	if got != "sk-test" {
		t.Fatalf("got %q", got)
	}
}

func TestPickFreePortHoldsReservation(t *testing.T) {
	port, hold, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("invalid port %d", port)
	}
	// While held, the port must not be claimable by anyone else.
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		ln.Close()
		hold.Close()
		t.Fatal("port was not reserved while the hold was open")
	}
	if err := hold.Close(); err != nil {
		t.Fatalf("close hold: %v", err)
	}
	// After release the browser can bind it.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port not usable after release: %v", err)
	}
	ln.Close()
}

func TestFindChromiumBinaryEnvOverride(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-browser")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMUX_BROWSER_BINARY", bin)
	got, err := findChromiumBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}

	t.Setenv("AMUX_BROWSER_BINARY", filepath.Join(t.TempDir(), "missing"))
	if _, err := findChromiumBinary(); err == nil {
		t.Fatal("expected error for missing AMUX_BROWSER_BINARY target")
	}
}

func TestTerminateBrowserNilSafe(t *testing.T) {
	terminateBrowser(nil)
	terminateBrowser(&exec.Cmd{})
}
