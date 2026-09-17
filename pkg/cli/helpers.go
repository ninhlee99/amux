package cli

import (
	"os"
	"path/filepath"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
)

func autoMigrateCheck() {
	// One-time automatic non-destructive migration from legacy ~/.am to ~/.amux
	home, _ := os.UserHomeDir()
	amDir := filepath.Join(home, ".am")
	amuxDir := filepath.Join(home, ".amux")
	if _, err := os.Stat(amuxDir); os.IsNotExist(err) {
		if _, err := os.Stat(amDir); err == nil {
			_ = copyDirNonDestructive(amDir, amuxDir)
		}
	}

	// One-time automatic non-destructive migration if identities.json does not exist
	idPath := identity.DefaultIdentitiesPath()
	if _, err := os.Stat(idPath); os.IsNotExist(err) {
		_, _ = identity.MigrateLegacyAccounts("", idPath)
	}
}

func copyDirNonDestructive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func toolAndName(args []string) (string, string) {
	if len(args) == 0 {
		return "claude", ""
	}
	first := args[0]
	if first == "claude" || first == "codex" || first == "gemini" || first == "antigravity" || first == "cursor" {
		if len(args) > 1 {
			return first, strings.Join(args[1:], " ")
		}
		return first, ""
	}
	// If first argument looks like a unified provider ID with known prefix, map to appropriate tool
	if strings.HasPrefix(first, "codexcli:") {
		return "codex", strings.Join(args, " ")
	}
	if strings.HasPrefix(first, "geminicli:") {
		return "antigravity", strings.Join(args, " ")
	}
	return "claude", strings.Join(args, " ")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func profileName(name, account string) string {
	if strings.TrimSpace(name) != "" {
		return strings.ReplaceAll(strings.TrimSpace(name), " ", "-")
	}
	return account
}

func isProviderName(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "claude" || s == "codex" || s == "gemini" || s == "antigravity" || s == "cursor" ||
		strings.HasPrefix(s, "claude-") || strings.HasPrefix(s, "codex-") || strings.HasPrefix(s, "gemini-")
}

func resolveName(tool, name string) string {
	if name != "" {
		return name
	}
	cfg := profile.LoadConfig()
	spec, ok := cfg.Tools[tool]
	if !ok {
		return ""
	}
	return profile.DetectAccount(spec)
}

func matchProviderID(target string) string {
	id, err := provider.MatchID(provider.DefaultAccountsPath(), target)
	if err == nil {
		return id
	}
	return target
}
