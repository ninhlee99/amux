package profile

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amux-accounts/pkg/types"
)

func fakeCodexAuth(email string) []byte {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"` + email + `"}`))
	return []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"e30.` + claims + `.sig","refresh_token":"rt-` + email + `"}}`)
}

// Two Codex subscriptions saved once each; `amux switch` swaps the live
// ~/.codex/auth.json between them with no new login.
func TestMultipleSubscriptions_SwitchWithoutLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_HOME", filepath.Join(home, ".amux"))
	authPath := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, email := range []string{"a@x.com", "b@x.com"} {
		data := fakeCodexAuth(email)
		if err := os.WriteFile(authPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		entries := []types.ProfileEntry{{
			Artifact: types.Artifact{Kind: "file", Path: authPath, AccountField: "jwt:tokens.id_token:email"},
			Data:     data,
		}}
		if err := SaveDirectProfile("codex", email, email, entries); err != nil {
			t.Fatalf("save %s: %v", email, err)
		}
	}
	if n := len(ListProfiles("codex")); n != 2 {
		t.Fatalf("want 2 saved codex profiles, got %d", n)
	}

	for _, want := range []string{"a@x.com", "b@x.com", "a@x.com"} {
		if err := CmdUse("codex", SanitizeName(want)); err != nil {
			t.Fatalf("switch to %s: %v", want, err)
		}
		got, _ := os.ReadFile(authPath)
		if !strings.Contains(string(got), "rt-"+want) {
			t.Fatalf("after switch to %s, auth.json = %s", want, got)
		}
	}
}
