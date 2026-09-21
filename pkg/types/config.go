package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// BaseDir returns the root storage directory (~/.amux or ~/.am, or overridden via
// $AMUX_HOME, $AM_HOME, or $AM_DIR).
func BaseDir() string {
	if d := os.Getenv("AMUX_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("AM_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("AM_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	amuxDir := filepath.Join(home, ".amux")
	if _, err := os.Stat(amuxDir); err == nil {
		return amuxDir
	}
	amDir := filepath.Join(home, ".am")
	if _, err := os.Stat(amDir); err == nil {
		return amDir
	}
	return amuxDir
}

// CurrentUser returns the current OS username.
func CurrentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

// ToolConfig describes all configured tools and their artifacts.
type ToolConfig struct {
	Tools map[string]ToolSpec `json:"tools"`
}

// DefaultToolConfig returns the built-in tool specifications for Claude, Codex, and Gemini.
func DefaultToolConfig() ToolConfig {
	home, _ := os.UserHomeDir()
	j := func(p string) string { return filepath.Join(home, p) }
	return ToolConfig{Tools: map[string]ToolSpec{
		"claude": {Name: "claude", Artifacts: []Artifact{
			{Kind: "keychain", Service: "Claude Code-credentials", Account: CurrentUser(), Optional: true},
			{Kind: "file", Path: j(".claude.json"), Optional: true, AccountField: "oauthAccount.emailAddress"},
			{Kind: "file", Path: j(".claude/.credentials.json"), Optional: true, AccountField: "email"},
		}},
		"codex": {Name: "codex", Artifacts: []Artifact{
			{Kind: "file", Path: j(".codex/auth.json"), AccountField: "jwt:tokens.id_token:email"},
		}},
		"gemini": {Name: "gemini", Artifacts: []Artifact{
			{Kind: "keychain", Service: "gemini", Account: "antigravity", Optional: true, AccountField: "jwt:id_token:email"},
			{Kind: "file", Path: j(".gemini/oauth_creds.json"), Optional: true, AccountField: "jwt:id_token:email"},
			{Kind: "file", Path: j(".gemini/google_accounts.json"), Optional: true, AccountField: "active"},
			{Kind: "file", Path: j(".gemini/installation_id"), Optional: true},
		}},
		"antigravity": {Name: "antigravity", Artifacts: []Artifact{
			{Kind: "keychain", Service: "gemini", Account: "antigravity", Optional: true, AccountField: "jwt:id_token:email"},
			{Kind: "file", Path: j(".gemini/google_accounts.json"), Optional: true, AccountField: "active"},
			{Kind: "file", Path: j(".gemini/installation_id"), Optional: true},
		}},
	}}
}

// ProjectSlug converts a project root path into a safe, filesystem-friendly directory name
// with a deterministic 8-character hash suffix for uniqueness across paths with the same basename.
func ProjectSlug(projectRoot string) string {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return "global"
	}
	clean := filepath.Clean(projectRoot)
	base := filepath.Base(clean)
	if base == "" || base == "/" || base == "." {
		base = "root"
	}
	// Sanitize base name
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	cleanBase := strings.Trim(b.String(), "-")
	if cleanBase == "" {
		cleanBase = "project"
	}
	h := sha256.Sum256([]byte(clean))
	return fmt.Sprintf("%s-%s", cleanBase, hex.EncodeToString(h[:4]))
}

// ProjectCacheDir returns the per-project storage directory (~/.am/projects/<slug>).
func ProjectCacheDir(projectRoot string) string {
	return filepath.Join(BaseDir(), "projects", ProjectSlug(projectRoot))
}

