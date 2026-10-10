package browser

import (
	"testing"
)

func TestParseGeminiAccountEmail(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "wiz_global_data",
			html: `<html><script>window.WIZ_global_data = {"nVwyhb":false,"nmYmKe":"gemini-web-uiserver","oPEP7c":"alice@gmail.com","oUbv8e":false};</script></html>`,
			want: "alice@gmail.com",
		},
		{
			name: "aria_label_vietnamese",
			html: `<div class="gb_Hb"><a class="gb_bb" aria-label="Tài khoản Google: Bi UTE &#10;(bi117.ute@gmail.com)" href="https://accounts.google.com"></a></div>`,
			want: "bi117.ute@gmail.com",
		},
		{
			name: "aria_label_english",
			html: `<div class="gb_Hb"><a class="gb_bb" aria-label="Google Account: John Doe&#10;(john.doe@example.org)" href="https://accounts.google.com"></a></div>`,
			want: "john.doe@example.org",
		},
		{
			name: "google_bar_dropdown",
			html: `<div class="gb_1"><div class="gb_4c"><div>Tài khoản Google</div><div class="gb_g">John</div><div>john@domain.net</div></div></div>`,
			want: "john@domain.net",
		},
		{
			name: "skip_google_system_emails",
			html: `<!-- contact googlers@google.com --><div><span>real.user@gmail.com</span></div>`,
			want: "real.user@gmail.com",
		},
		{
			name: "empty_or_unauthenticated",
			html: `<html><body>Sign in to Google</body></html>`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGeminiAccountEmail(tc.html)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseGeminiAccountPlan(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "advanced_english",
			html: `<div>Welcome to Gemini Advanced</div>`,
			want: "pro",
		},
		{
			name: "google_one_vietnamese",
			html: `<div>Gói thành viên của Google</div>`,
			want: "pro",
		},
		{
			name: "google_one_english",
			html: `<div>Subscribed via Google One</div>`,
			want: "pro",
		},
		{
			name: "free_account",
			html: `<div>Gemini Free Tier</div>`,
			want: "free",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGeminiAccountPlan(tc.html)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
