package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func TestCheckSessionAccount(t *testing.T) {
	if err := checkSessionAccount("chatgpt:a", "bi117.ute@gmail.com", "ninhle21199@gmail.com"); !errors.Is(err, types.ErrAuthentication) {
		t.Fatalf("another account's session must be rejected, got %v", err)
	}
	for _, c := range [][2]string{{"a@x.com", "A@X.com"}, {"", "b@x.com"}, {"a@x.com", ""}, {"-", "b@x.com"}} {
		if err := checkSessionAccount("id", c[0], c[1]); err != nil {
			t.Errorf("%q vs %q: %v", c[0], c[1], err)
		}
	}
}

// A browser refresh that lands on another Google account is rolled back.
func TestGeminiInit_RejectsOtherAccountAfterRefresh(t *testing.T) {
	a := &GeminiWebAdapter{
		AdapterID:   "gemini:web:a",
		Account:     "a@gmail.com",
		Cookies:     "__Secure-1PSID=new",
		prevCookies: "__Secure-1PSID=old",
		HTTPClient:  &http.Client{Transport: staticPage(`"SNlM0e":"tok","oPEP7c":"b@gmail.com"`)},
	}
	if err := a.initLocked(context.Background(), true); !errors.Is(err, types.ErrAuthentication) {
		t.Fatalf("want auth error, got %v", err)
	}
	if a.Cookies != "__Secure-1PSID=old" || a.inited {
		t.Fatalf("cookies must be restored, got %q inited=%v", a.Cookies, a.inited)
	}
}

type staticPage string

func (p staticPage) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(p))), Header: http.Header{}}, nil
}

// Two accounts of one product signed in to different browser profiles: each
// slot recovers its own session, whatever profile was used last.
func TestPickBrowserSession_MatchesAccount(t *testing.T) {
	sessions := []browserSession{{Value: "s-ninh", Jar: "j-ninh"}, {Value: "s-bi", Jar: "j-bi"}}
	whoami := func(s browserSession) (string, error) {
		if s.Value == "s-bi" {
			return "bi117.ute@gmail.com", nil
		}
		return "ninhle21199@gmail.com", nil
	}
	got, err := pickBrowserSession("chatgpt:bi", "bi117.ute@gmail.com", sessions, whoami)
	if err != nil || got.Value != "s-bi" {
		t.Fatalf("want bi117ute's session, got %+v %v", got, err)
	}
	if got, err := pickBrowserSession("chatgpt:x", "", sessions, whoami); err != nil || got.Value != "s-ninh" {
		t.Fatalf("unknown account takes the most recent session, got %+v %v", got, err)
	}
	if _, err := pickBrowserSession("chatgpt:z", "z@x.com", sessions, whoami); !errors.Is(err, types.ErrAuthentication) {
		t.Fatalf("no matching profile must be an auth error, got %v", err)
	}
}

func TestStripChatGPTCitations(t *testing.T) {
	in := "Title is \"Example Domains\" (IANA). citeturn0search0\nNext"
	if got := stripChatGPTCitations(in); got != "Title is \"Example Domains\" (IANA). \nNext" {
		t.Fatalf("got %q", got)
	}
	if got := stripChatGPTCitations("partial citetur"); got != "partial " {
		t.Fatalf("unfinished marker must be held back, got %q", got)
	}
}
