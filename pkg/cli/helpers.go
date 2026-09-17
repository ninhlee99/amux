package cli

import (
	"os"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/profile"
	"amux-accounts/pkg/provider"
)

func autoMigrateCheck() {
	// One-time automatic non-destructive migration if identities.json does not exist
	idPath := identity.DefaultIdentitiesPath()
	if _, err := os.Stat(idPath); os.IsNotExist(err) {
		_, _ = identity.MigrateLegacyAccounts("", idPath)
	}
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
