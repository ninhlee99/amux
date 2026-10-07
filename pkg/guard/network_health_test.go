package guard

import (
	"errors"
	"fmt"
	"testing"

	"amux-accounts/pkg/types"
)

// A Cloudflare challenge after the network dropped reads "403"; it used to
// count as an auth failure and quarantined the account for an hour.
func TestRecordError_NetworkNeverQuarantines(t *testing.T) {
	h := NewHealthTracker()
	cf := fmt.Errorf("chatgpt:x: sentinel: HTTP 403 Cloudflare challenge: %w", types.ErrUpstreamUnreachable)
	untyped := errors.New("upstream status 403: <html>... window._cf_chl_opt ...")
	dial := errors.New("dial tcp: lookup chatgpt.com: no such host")
	for i := 0; i < 5; i++ {
		h.RecordError("chatgpt:x", cf)
		h.RecordError("chatgpt:x", untyped)
		h.RecordError("chatgpt:x", dial)
	}
	if q, _, reason := h.IsQuarantined("chatgpt:x"); q {
		t.Fatalf("network errors quarantined the account: %s", reason)
	}
	if r := h.GetReport("chatgpt:x"); r.Score != 100 || r.ConsecutiveAuthErr != 0 {
		t.Fatalf("network errors hurt the account: %+v", r)
	}
}

func TestClassifyError_Plain403StaysAuth(t *testing.T) {
	if c := classifyError(errors.New("upstream status 403: forbidden")); c != classAuth {
		t.Fatalf("plain 403 class = %v, want auth", c)
	}
}
