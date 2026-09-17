package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amux-accounts/pkg/identity"
)

const GatewayDefaultURL = "http://127.0.0.1:8787"

// HookTarget defines supported IDEs for gateway hook injection.
type HookTarget string

const (
	TargetClaude HookTarget = "claude"
	TargetCursor HookTarget = "cursor"
	TargetCodex  HookTarget = "codex"
	TargetAll    HookTarget = "all"
)

// ClaudeSettingsPath returns ~/.claude/settings.json.
func ClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// HookClaude injects ANTHROPIC_BASE_URL into ~/.claude/settings.json.
func HookClaude(baseURL string) error {
	if baseURL == "" {
		baseURL = GatewayDefaultURL
	}
	p := ClaudeSettingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}

	envMap, _ := m["env"].(map[string]any)
	if envMap == nil {
		envMap = make(map[string]any)
	}
	envMap["ANTHROPIC_BASE_URL"] = baseURL
	m["env"] = envMap

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// UnhookClaude removes ANTHROPIC_BASE_URL from ~/.claude/settings.json.
func UnhookClaude() error {
	p := ClaudeSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}

	envMap, ok := m["env"].(map[string]any)
	if !ok || envMap == nil {
		return nil
	}
	if _, exists := envMap["ANTHROPIC_BASE_URL"]; !exists {
		return nil
	}
	delete(envMap, "ANTHROPIC_BASE_URL")
	if len(envMap) == 0 {
		delete(m, "env")
	} else {
		m["env"] = envMap
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// IsClaudeHooked checks if Claude Code is currently pointed at the gateway.
func IsClaudeHooked() (bool, string) {
	p := ClaudeSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return false, ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false, ""
	}
	if envMap, ok := m["env"].(map[string]any); ok {
		if val, ok := envMap["ANTHROPIC_BASE_URL"].(string); ok && strings.TrimSpace(val) != "" {
			return true, val
		}
	}
	return false, ""
}

// CodexConfigPath returns ~/.codex/config.json.
func CodexConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.json")
}

// HookCodex points Codex CLI at the gateway.
func HookCodex(baseURL string) error {
	if baseURL == "" {
		baseURL = GatewayDefaultURL + "/v1"
	}
	p := CodexConfigPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["openai_base_url"] = baseURL

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// UnhookCodex removes gateway hook from ~/.codex/config.json.
func UnhookCodex() error {
	p := CodexConfigPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	if _, exists := m["openai_base_url"]; !exists {
		return nil
	}
	delete(m, "openai_base_url")

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// IsCodexHooked checks if Codex is currently hooked to the gateway.
func IsCodexHooked() (bool, string) {
	p := CodexConfigPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return false, ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false, ""
	}
	if val, ok := m["openai_base_url"].(string); ok && strings.TrimSpace(val) != "" {
		return true, val
	}
	return false, ""
}

// CursorSettingsPath returns Cursor's settings.json path on macOS.
func CursorSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "settings.json")
}

// HookCursor points Cursor at the gateway.
func HookCursor(baseURL string) error {
	if baseURL == "" {
		baseURL = GatewayDefaultURL + "/v1"
	}
	p := CursorSettingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["cursor.openaiBaseUrl"] = baseURL

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// UnhookCursor removes Cursor hook.
func UnhookCursor() error {
	p := CursorSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	if _, exists := m["cursor.openaiBaseUrl"]; !exists {
		return nil
	}
	delete(m, "cursor.openaiBaseUrl")

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// IsCursorHooked checks if Cursor is hooked.
func IsCursorHooked() (bool, string) {
	p := CursorSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return false, ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false, ""
	}
	if val, ok := m["cursor.openaiBaseUrl"].(string); ok && strings.TrimSpace(val) != "" {
		return true, val
	}
	return false, ""
}

// Hook applies hooks to the specified target.
func Hook(target HookTarget, baseURL string) error {
	switch target {
	case TargetClaude:
		return HookClaude(baseURL)
	case TargetCodex:
		return HookCodex(baseURL)
	case TargetCursor:
		return HookCursor(baseURL)
	case TargetAll:
		_ = HookClaude(baseURL)
		_ = HookCodex(baseURL)
		_ = HookCursor(baseURL)
		return nil
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}

// Unhook removes hooks from the specified target.
func Unhook(target HookTarget) error {
	switch target {
	case TargetClaude:
		return UnhookClaude()
	case TargetCodex:
		return UnhookCodex()
	case TargetCursor:
		return UnhookCursor()
	case TargetAll:
		_ = UnhookClaude()
		_ = UnhookCodex()
		_ = UnhookCursor()
		return nil
	default:
		return fmt.Errorf("unknown unhook target: %s", target)
	}
}

// CheckAndConditionalHook evaluates identities and conditionally injects or detaches gateway hooks:
// 1. Conditional Gateway Injection: ONLY when ALL subscription accounts of a provider reach threshold,
//    inject the gateway hook into the IDE's settings.
// 2. Auto-Detachment: As soon as any subscription account resets quota (usage_percent < threshold),
//    AMUX automatically detaches the gateway hook from the IDE and restores direct native execution.
func CheckAndConditionalHook(identities []identity.Identity, threshold float64) error {
	providers := []string{"anthropic", "openai"}

	for _, prov := range providers {
		canon := identity.CanonicalProvider(prov)
		exhausted := identity.AllSubscriptionsExhausted(canon, identities, threshold)
		available, hasAvail := identity.HasAvailableSubscription(canon, identities, threshold)

		switch canon {
		case "anthropic":
			hooked, _ := IsClaudeHooked()
			if exhausted && !hooked {
				// All subscriptions exhausted -> Inject gateway hook!
				_ = HookClaude("")
			} else if hasAvail && hooked {
				// Quota reset or subscription available -> Auto-detach hook and restore direct Keychain!
				_ = UnhookClaude()
				_ = identity.SyncIdentityToNativeKeychain(available)
			}
		case "openai":
			hooked, _ := IsCodexHooked()
			if exhausted && !hooked {
				_ = HookCodex("")
			} else if hasAvail && hooked {
				_ = UnhookCodex()
				_ = identity.SyncIdentityToNativeKeychain(available)
			}
		}
	}
	return nil
}
