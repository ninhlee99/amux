package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

// fakeGateway answers like the real gateway; toolReply controls whether the
// tool roundtrip gets a tool_use block or prose.
func fakeGateway(t *testing.T, toolReply bool, pins *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(types.RequestIDHeader, r.Header.Get(types.RequestIDHeader))
		if pins != nil && r.URL.Path != "/_am/status" {
			*pins = append(*pins, r.Header.Get("X-Provider"))
		}
		switch r.URL.Path {
		case "/_am/status":
			_, _ = io.WriteString(w, `{}`)
		case "/v1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, hasTools := body["tools"]; hasTools && toolReply {
				_, _ = io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Bash","input":{"command":"echo amux-live-ok"}}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"OK"}]}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunLiveChecks_AllPass(t *testing.T) {
	var pins []string
	srv := fakeGateway(t, true, &pins)
	defer srv.Close()
	res := runLiveChecks(srv.URL, "chatgpt:01", srv.Client())
	if len(res) != 4 {
		t.Fatalf("want 4 checks, got %d", len(res))
	}
	for _, r := range res {
		if !r.OK {
			t.Fatalf("%s failed: %s", r.Name, r.Detail)
		}
	}
	if !strings.HasPrefix(res[1].ReqID, "ax_") {
		t.Fatalf("request id not reported: %+v", res[1])
	}
	for _, p := range pins {
		if p != "chatgpt:01" {
			t.Fatalf("--provider not sent on every turn: %v", pins)
		}
	}
}

func TestRunLiveChecks_ProseInsteadOfToolFails(t *testing.T) {
	srv := fakeGateway(t, false, nil)
	defer srv.Close()
	res := runLiveChecks(srv.URL, "", srv.Client())
	tool := res[len(res)-1]
	if tool.OK || !strings.Contains(tool.Detail, "no tool_use") || len(tool.Fix) == 0 {
		t.Fatalf("tool check should fail with a fix: %+v", tool)
	}
}

func TestRunLiveChecks_GatewayDownStopsEarly(t *testing.T) {
	srv := fakeGateway(t, true, nil)
	url := srv.URL
	srv.Close()
	res := runLiveChecks(url, "", http.DefaultClient)
	if len(res) != 1 || res[0].OK || res[0].Fix[0] != "amux start" {
		t.Fatalf("gateway down: %+v", res)
	}
}

func TestRunLiveChecks_ExpiredSessionSuggestsLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_am/status" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"session expired"}}`)
	}))
	defer srv.Close()
	res := runLiveChecks(srv.URL, "", srv.Client())
	if res[1].OK || !strings.Contains(strings.Join(res[1].Fix, "\n"), "amux login") {
		t.Fatalf("401 should suggest login: %+v", res[1])
	}
}
