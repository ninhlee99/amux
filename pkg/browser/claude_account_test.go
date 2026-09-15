package browser

import "testing"

func TestParseClaudeAccountEmail(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"flat", `{"email":"ninhle@x.com","id":"1"}`, "ninhle@x.com"},
		{"account", `{"account":{"email":"tungnt@y.com"}}`, "tungnt@y.com"},
		{"user", `{"user":{"email":"a@b.com"}}`, "a@b.com"},
		{"empty", `{}`, ""},
		{"bad", `not-json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseClaudeAccountEmail([]byte(tc.body)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseClaudeAccountPlan_MaxNotFree(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"claude_max",
			`{"memberships":[{"organization":{"analytics_subscription_plan":"claude_max","billing_type":"stripe_subscription"}}]}`,
			"max",
		},
		{
			"claude_pro",
			`{"memberships":[{"organization":{"analytics_subscription_plan":"claude_pro","billing_type":"stripe_subscription"}}]}`,
			"pro",
		},
		{
			"free",
			`{"memberships":[{"organization":{"analytics_subscription_plan":"claude_free","billing_type":"none"}}]}`,
			"free",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseClaudeAccountPlan([]byte(tc.body)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
