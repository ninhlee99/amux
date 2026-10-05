package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"amux-accounts/pkg/types"
)

// Switching Claude accounts must only swap oauthAccount in ~/.claude.json;
// MCP servers and project state added since the snapshot stay.
func TestApplyEntry_ClaudeJSONKeepsUserState(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	live := `{"oauthAccount":{"emailAddress":"a@x.com"},"mcpServers":{"amux":{"command":"amux"}},"projects":{"/p":{}}}`
	if err := os.WriteFile(path, []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := `{"oauthAccount":{"emailAddress":"b@x.com"},"mcpServers":{}}`
	e := types.ProfileEntry{Artifact: types.Artifact{Kind: "file", Path: path}, Data: []byte(snap)}
	if err := ApplyEntry(e); err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["oauthAccount"]["emailAddress"] != "b@x.com" {
		t.Fatalf("account not switched: %s", b)
	}
	if doc["mcpServers"]["amux"] == nil || doc["projects"] == nil {
		t.Fatalf("user state lost: %s", b)
	}
}
