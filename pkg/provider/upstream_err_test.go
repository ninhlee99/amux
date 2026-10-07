package provider

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

// ChatGPT's challenge page starts with ~6KB of inline SVG, so the
// cf_chl marker sat past the 2KB the adapter used to read.
func TestUpstreamHTTPError_CloudflareChallengeIsUnreachable(t *testing.T) {
	page := "<html><head></head><body><svg>" + strings.Repeat("M37.5 16.8 ", 800) +
		"</svg><script>window._cf_chl_opt={cRay:'429tpm'}</script></body></html>"
	resp := &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"text/html"}}}
	err := upstreamHTTPError("sentinel", resp, []byte(page))
	if !errors.Is(err, types.ErrUpstreamUnreachable) {
		t.Fatalf("challenge page not marked unreachable: %v", err)
	}
	if strings.Contains(err.Error(), "<html") || strings.Contains(err.Error(), "429") {
		t.Fatalf("error leaks the page: %q", err)
	}

	byHeader := &http.Response{StatusCode: 403, Header: http.Header{"Cf-Mitigated": {"challenge"}}}
	if err := upstreamHTTPError("conversation", byHeader, []byte("<html>")); !errors.Is(err, types.ErrUpstreamUnreachable) {
		t.Fatalf("cf-mitigated header not marked unreachable: %v", err)
	}
}

func TestUpstreamHTTPError_OtherHTMLIsSummarized(t *testing.T) {
	resp := &http.Response{StatusCode: 500, Header: http.Header{}}
	err := upstreamHTTPError("upstream", resp, []byte("<!DOCTYPE html><html>oops</html>"))
	if errors.Is(err, types.ErrUpstreamUnreachable) || err.Error() != "upstream status 500: HTML error page" {
		t.Fatalf("got %v", err)
	}
}

func TestWrapNetworkError(t *testing.T) {
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network is unreachable")}
	if err := wrapNetworkError(dial); !errors.Is(err, types.ErrUpstreamUnreachable) {
		t.Fatalf("dial error not unreachable: %v", err)
	}
	other := errors.New("boom")
	if err := wrapNetworkError(other); errors.Is(err, types.ErrUpstreamUnreachable) {
		t.Fatalf("plain error wrapped: %v", err)
	}
}
