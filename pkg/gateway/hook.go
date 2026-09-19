package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	TargetAgy    HookTarget = "agy"
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

var (
	reCodexBaseURL   = regexp.MustCompile(`(?m)^[ \t]*openai_base_url[ \t]*=.*$`)
	reFirstTOMLTable = regexp.MustCompile(`(?m)^\[.+\]`)
)

// CodexConfigPath returns ~/.codex/config.json.
func CodexConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.json")
}

// CodexTomlPath returns ~/.codex/config.toml.
func CodexTomlPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.toml")
}

// HookCodex points Codex CLI at the gateway.
func HookCodex(baseURL string) error {
	if baseURL == "" {
		baseURL = GatewayDefaultURL + "/v1"
	}

	// 1. Update ~/.codex/config.json
	p := CodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["openai_base_url"] = baseURL
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, append(b, '\n'), 0o600); err != nil {
		return err
	}

	// 2. Update ~/.codex/config.toml
	tomlPath := CodexTomlPath()
	line := fmt.Sprintf("openai_base_url = %q", baseURL)
	if tomlBytes, err := os.ReadFile(tomlPath); err == nil {
		tomlStr := string(tomlBytes)
		if reCodexBaseURL.MatchString(tomlStr) {
			tomlStr = reCodexBaseURL.ReplaceAllString(tomlStr, line)
		} else {
			// Place at root table scope before any [section] header
			if loc := reFirstTOMLTable.FindStringIndex(tomlStr); loc != nil {
				prefix := tomlStr[:loc[0]]
				if prefix != "" && !strings.HasSuffix(prefix, "\n") {
					prefix += "\n"
				}
				tomlStr = prefix + line + "\n\n" + tomlStr[loc[0]:]
			} else {
				if tomlStr != "" && !strings.HasSuffix(tomlStr, "\n") {
					tomlStr += "\n"
				}
				tomlStr += line + "\n"
			}
		}
		if err := os.WriteFile(tomlPath, []byte(tomlStr), 0o600); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(tomlPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(tomlPath, []byte(line+"\n"), 0o600); err != nil {
			return err
		}
	} else {
		return err
	}

	// 3. Update environment for session / launchctl
	_ = exec.Command("launchctl", "setenv", "OPENAI_BASE_URL", baseURL).Run()
	return nil
}

// UnhookCodex removes gateway hook from ~/.codex/config.json, config.toml, and launchctl.
func UnhookCodex() error {
	// 1. Remove from ~/.codex/config.json
	p := CodexConfigPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err == nil {
			if _, exists := m["openai_base_url"]; exists {
				delete(m, "openai_base_url")
				data, err := json.MarshalIndent(m, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(p, append(data, '\n'), 0o600); err != nil {
					return err
				}
			}
		}
	}

	// 2. Remove from ~/.codex/config.toml
	tomlPath := CodexTomlPath()
	if tomlBytes, err := os.ReadFile(tomlPath); err == nil {
		tomlStr := string(tomlBytes)
		if reCodexBaseURL.MatchString(tomlStr) {
			tomlStr = reCodexBaseURL.ReplaceAllString(tomlStr, "")
			tomlStr = strings.TrimLeft(tomlStr, "\r\n")
			if err := os.WriteFile(tomlPath, []byte(tomlStr), 0o600); err != nil {
				return err
			}
		}
	}

	// 3. Remove launchctl env
	_ = exec.Command("launchctl", "unsetenv", "OPENAI_BASE_URL").Run()
	return nil
}

// IsCodexHooked checks if Codex is currently hooked to the gateway.
func IsCodexHooked() (bool, string) {
	// 1. Check config.toml
	tomlPath := CodexTomlPath()
	if b, err := os.ReadFile(tomlPath); err == nil {
		if m := reCodexBaseURL.FindString(string(b)); m != "" {
			parts := strings.SplitN(m, "=", 2)
			if len(parts) == 2 {
				val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				if val != "" {
					return true, val
				}
			}
		}
	}

	// 2. Check config.json
	p := CodexConfigPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err == nil {
			if val, ok := m["openai_base_url"].(string); ok && strings.TrimSpace(val) != "" {
				return true, val
			}
		}
	}

	// 3. Check launchctl
	out, err := exec.Command("launchctl", "getenv", "OPENAI_BASE_URL").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return true, strings.TrimSpace(string(out))
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

// AGYSettingsPath returns ~/.gemini/antigravity-cli/settings.json.
func AGYSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
}

// HookAgy points Antigravity CLI (agy) at the gateway.
func HookAgy(baseURL string) error {
	if baseURL == "" {
		baseURL = GatewayDefaultURL
	}
	p := AGYSettingsPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["modelProvider"] = "gemini"

	envMap, _ := m["env"].(map[string]any)
	if envMap == nil {
		envMap = make(map[string]any)
	}
	envMap["GOOGLE_GEMINI_BASE_URL"] = baseURL
	envMap["GEMINI_API_KEY"] = "amux-local"
	m["env"] = envMap

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, append(b, '\n'), 0o600); err != nil {
		return err
	}

	_ = exec.Command("launchctl", "setenv", "GOOGLE_GEMINI_BASE_URL", baseURL).Run()
	_ = exec.Command("launchctl", "setenv", "GEMINI_API_KEY", "amux-local").Run()
	return nil
}

// UnhookAgy restores native execution for Antigravity CLI.
func UnhookAgy() error {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			delete(m, "modelProvider")
			if envMap, ok := m["env"].(map[string]any); ok {
				delete(envMap, "GOOGLE_GEMINI_BASE_URL")
				delete(envMap, "GEMINI_API_KEY")
				if len(envMap) == 0 {
					delete(m, "env")
				} else {
					m["env"] = envMap
				}
			}
			if data, err := json.MarshalIndent(m, "", "  "); err == nil {
				_ = os.WriteFile(p, append(data, '\n'), 0o600)
			}
		}
	}
	_ = exec.Command("launchctl", "unsetenv", "GOOGLE_GEMINI_BASE_URL").Run()
	_ = exec.Command("launchctl", "unsetenv", "GEMINI_API_KEY").Run()
	return nil
}

// IsAgyHooked checks if Antigravity CLI is currently hooked to the gateway.
func IsAgyHooked() (bool, string) {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if envMap, ok := m["env"].(map[string]any); ok {
				if val, ok := envMap["GOOGLE_GEMINI_BASE_URL"].(string); ok && strings.TrimSpace(val) != "" {
					return true, val
				}
			}
		}
	}
	out, err := exec.Command("launchctl", "getenv", "GOOGLE_GEMINI_BASE_URL").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return true, strings.TrimSpace(string(out))
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
	case TargetAgy:
		return HookAgy(baseURL)
	case TargetAll:
		_ = HookClaude(baseURL)
		_ = HookCodex(baseURL)
		_ = HookCursor(baseURL)
		_ = HookAgy(baseURL)
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
	case TargetAgy:
		return UnhookAgy()
	case TargetAll:
		_ = UnhookClaude()
		_ = UnhookCodex()
		_ = UnhookCursor()
		_ = UnhookAgy()
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
	providers := []string{"anthropic", "openai", "gemini"}

	for _, prov := range providers {
		canon := identity.CanonicalProvider(prov)
		exhausted := identity.AllSubscriptionsExhausted(canon, identities, threshold)
		available, hasAvail := identity.HasAvailableSubscription(canon, identities, threshold)

		switch canon {
		case "anthropic":
			hooked, _ := IsClaudeHooked()
			if exhausted && !hooked {
				// All subscriptions exhausted -> Inject gateway hook!
				if err := HookClaude(""); err != nil {
					return fmt.Errorf("hook claude: %w", err)
				}
			} else if hasAvail && hooked {
				// Quota reset or subscription available -> Auto-detach hook and restore direct Keychain!
				if err := UnhookClaude(); err != nil {
					return fmt.Errorf("unhook claude: %w", err)
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					return fmt.Errorf("sync claude keychain: %w", err)
				}
			}
		case "openai":
			hooked, _ := IsCodexHooked()
			if exhausted && !hooked {
				if err := HookCodex(""); err != nil {
					return fmt.Errorf("hook codex: %w", err)
				}
			} else if hasAvail && hooked {
				if err := UnhookCodex(); err != nil {
					return fmt.Errorf("unhook codex: %w", err)
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					return fmt.Errorf("sync codex keychain: %w", err)
				}
			}
		case "gemini":
			hooked, _ := IsAgyHooked()
			if exhausted && !hooked {
				if err := HookAgy(""); err != nil {
					return fmt.Errorf("hook agy: %w", err)
				}
			} else if hasAvail && hooked {
				if err := UnhookAgy(); err != nil {
					return fmt.Errorf("unhook agy: %w", err)
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					return fmt.Errorf("sync agy keychain: %w", err)
				}
			}
		}
	}
	return nil
}
