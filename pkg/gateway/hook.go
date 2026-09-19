package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

var (
	reCodexBaseURL   = regexp.MustCompile(`(?m)^[ \t]*openai_base_url[ \t]*=.*$`)
	reFirstTOMLTable = regexp.MustCompile(`(?m)^\[.+\]`)
)

func isGatewayURL(v string) bool {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	port := u.Port()
	if port != "8787" {
		return false
	}
	return host == "127.0.0.1" || host == "localhost" || host == "0.0.0.0"
}

// isAmuxOwnedEnv returns true if key/value was configured by amux.
// For base URLs, it checks if the value points to the amux gateway.
// For API keys, it checks for amux placeholder credentials ("amux-local", "amux").
func isAmuxOwnedEnv(key, val string) bool {
	val = strings.TrimSpace(val)
	if val == "" {
		return false
	}
	if strings.HasSuffix(key, "_BASE_URL") || strings.HasSuffix(key, "BaseUrl") || strings.HasSuffix(key, "base_url") {
		return isGatewayURL(val)
	}
	if strings.HasSuffix(key, "_API_KEY") || strings.HasSuffix(key, "ApiKey") || strings.HasSuffix(key, "api_key") {
		return val == "amux-local" || val == "amux"
	}
	return false
}

func stageFile(path string, data []byte, perm os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "hook-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := stageFile(path, data, perm)
	if err != nil {
		return err
	}
	defer func() {
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}()
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	tmp = ""
	return nil
}

func setLaunchEnv(key, val string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("launchctl", "setenv", key, val).Run()
	}
}

func unsetLaunchEnv(key string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("launchctl", "unsetenv", key).Run()
	}
}

func getLaunchEnv(key string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("launchctl", "getenv", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

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

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
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
	return atomicWriteFile(p, append(b, '\n'), 0o600)
}

// UnhookClaude removes ANTHROPIC_BASE_URL from ~/.claude/settings.json.
func UnhookClaude() error {
	p := ClaudeSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("unmarshal %s: %w", p, err)
		}
	}

	envMap, ok := m["env"].(map[string]any)
	if !ok || envMap == nil {
		return nil
	}
	val, exists := envMap["ANTHROPIC_BASE_URL"].(string)
	if !exists || !isGatewayURL(val) {
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
	return atomicWriteFile(p, append(data, '\n'), 0o600)
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
		if val, ok := envMap["ANTHROPIC_BASE_URL"].(string); ok && isGatewayURL(val) {
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

	// 1. Prepare ~/.codex/config.json
	p := CodexConfigPath()
	var origJSON []byte
	var jsonExisted bool
	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		origJSON = b
		jsonExisted = true
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	m["openai_base_url"] = baseURL
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	newJSON := append(b, '\n')

	// 2. Prepare ~/.codex/config.toml
	tomlPath := CodexTomlPath()
	line := fmt.Sprintf("openai_base_url = %q", baseURL)
	var newTOML string
	if tomlBytes, err := os.ReadFile(tomlPath); err == nil {
		tomlStr := string(tomlBytes)
		loc := reFirstTOMLTable.FindStringIndex(tomlStr)
		if loc != nil {
			rootSection := tomlStr[:loc[0]]
			rest := tomlStr[loc[0]:]
			if reCodexBaseURL.MatchString(rootSection) {
				rootSection = reCodexBaseURL.ReplaceAllString(rootSection, line)
			} else {
				if rootSection != "" && !strings.HasSuffix(rootSection, "\n") {
					rootSection += "\n"
				}
				rootSection += line + "\n\n"
			}
			newTOML = rootSection + rest
		} else {
			if reCodexBaseURL.MatchString(tomlStr) {
				newTOML = reCodexBaseURL.ReplaceAllString(tomlStr, line)
			} else {
				if tomlStr != "" && !strings.HasSuffix(tomlStr, "\n") {
					tomlStr += "\n"
				}
				newTOML = tomlStr + line + "\n"
			}
		}
	} else if os.IsNotExist(err) {
		newTOML = line + "\n"
	} else {
		return err
	}

	// Stage both files atomically before committing either
	tmpJSON, err := stageFile(p, newJSON, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if tmpJSON != "" {
			_ = os.Remove(tmpJSON)
		}
	}()

	tmpTOML, err := stageFile(tomlPath, []byte(newTOML), 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if tmpTOML != "" {
			_ = os.Remove(tmpTOML)
		}
	}()

	// Commit JSON
	if err := os.Rename(tmpJSON, p); err != nil {
		return err
	}
	tmpJSON = ""

	// Commit TOML; if rename fails, rollback JSON
	if err := os.Rename(tmpTOML, tomlPath); err != nil {
		var rollbackErr error
		if jsonExisted {
			rollbackErr = atomicWriteFile(p, origJSON, 0o600)
		} else {
			rollbackErr = os.Remove(p)
		}
		if rollbackErr != nil {
			return fmt.Errorf("write %s: %w (rollback %s failed: %v)", tomlPath, err, p, rollbackErr)
		}
		return fmt.Errorf("write %s: %w (rolled back %s)", tomlPath, err, p)
	}
	tmpTOML = ""

	// 3. Update environment for session / launchctl
	setLaunchEnv("OPENAI_BASE_URL", baseURL)
	return nil
}

// UnhookCodex removes gateway hook from ~/.codex/config.json, config.toml, and launchctl.
func UnhookCodex() error {
	// 1. Remove from ~/.codex/config.json
	p := CodexConfigPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
			if val, exists := m["openai_base_url"].(string); exists && isGatewayURL(val) {
				delete(m, "openai_base_url")
				data, err := json.MarshalIndent(m, "", "  ")
				if err != nil {
					return err
				}
				if err := atomicWriteFile(p, append(data, '\n'), 0o600); err != nil {
					return err
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// 2. Remove from ~/.codex/config.toml
	tomlPath := CodexTomlPath()
	if tomlBytes, err := os.ReadFile(tomlPath); err == nil {
		tomlStr := string(tomlBytes)
		loc := reFirstTOMLTable.FindStringIndex(tomlStr)
		if loc != nil {
			rootSection := tomlStr[:loc[0]]
			rest := tomlStr[loc[0]:]
			if m := reCodexBaseURL.FindString(rootSection); m != "" {
				parts := strings.SplitN(m, "=", 2)
				if len(parts) == 2 && isGatewayURL(strings.Trim(strings.TrimSpace(parts[1]), `"'`)) {
					rootSection = reCodexBaseURL.ReplaceAllString(rootSection, "")
					rootSection = strings.TrimLeft(rootSection, "\r\n")
					tomlStr = rootSection + rest
					if err := atomicWriteFile(tomlPath, []byte(tomlStr), 0o600); err != nil {
						return err
					}
				}
			}
		} else {
			if m := reCodexBaseURL.FindString(tomlStr); m != "" {
				parts := strings.SplitN(m, "=", 2)
				if len(parts) == 2 && isGatewayURL(strings.Trim(strings.TrimSpace(parts[1]), `"'`)) {
					tomlStr = reCodexBaseURL.ReplaceAllString(tomlStr, "")
					tomlStr = strings.TrimLeft(tomlStr, "\r\n")
					if err := atomicWriteFile(tomlPath, []byte(tomlStr), 0o600); err != nil {
						return err
					}
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// 3. Remove launchctl env if pointed to gateway
	if isGatewayURL(getLaunchEnv("OPENAI_BASE_URL")) {
		unsetLaunchEnv("OPENAI_BASE_URL")
	}
	return nil
}

// IsCodexHooked checks if Codex is currently hooked to the gateway.
func IsCodexHooked() (bool, string) {
	// 1. Check config.toml
	tomlPath := CodexTomlPath()
	if b, err := os.ReadFile(tomlPath); err == nil {
		tomlStr := string(b)
		loc := reFirstTOMLTable.FindStringIndex(tomlStr)
		if loc != nil {
			tomlStr = tomlStr[:loc[0]]
		}
		if m := reCodexBaseURL.FindString(tomlStr); m != "" {
			parts := strings.SplitN(m, "=", 2)
			if len(parts) == 2 {
				val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				if isGatewayURL(val) {
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
			if val, ok := m["openai_base_url"].(string); ok && isGatewayURL(val) {
				return true, val
			}
		}
	}

	// 3. Check launchctl
	envVal := getLaunchEnv("OPENAI_BASE_URL")
	if isGatewayURL(envVal) {
		return true, envVal
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

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
	}
	m["cursor.openaiBaseUrl"] = baseURL

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(p, append(b, '\n'), 0o600)
}

// UnhookCursor removes Cursor hook.
func UnhookCursor() error {
	p := CursorSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("unmarshal %s: %w", p, err)
		}
	}
	val, exists := m["cursor.openaiBaseUrl"].(string)
	if !exists || !isGatewayURL(val) {
		return nil
	}
	delete(m, "cursor.openaiBaseUrl")

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(p, append(data, '\n'), 0o600)
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
	if val, ok := m["cursor.openaiBaseUrl"].(string); ok && isGatewayURL(val) {
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

	m := make(map[string]any)
	if b, err := os.ReadFile(p); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if mp, ok := m["modelProvider"].(string); !ok || mp == "" {
		m["modelProvider"] = "gemini"
	}

	envMap, _ := m["env"].(map[string]any)
	if envMap == nil {
		envMap = make(map[string]any)
	}
	if existingURL, ok := envMap["GOOGLE_GEMINI_BASE_URL"].(string); ok && existingURL != "" && !isGatewayURL(existingURL) {
		return fmt.Errorf("refusing to overwrite existing GOOGLE_GEMINI_BASE_URL (%s) not managed by amux", existingURL)
	}
	envMap["GOOGLE_GEMINI_BASE_URL"] = baseURL
	if existingKey, ok := envMap["GEMINI_API_KEY"].(string); !ok || existingKey == "" || isAmuxOwnedEnv("GEMINI_API_KEY", existingKey) {
		envMap["GEMINI_API_KEY"] = "amux-local"
	}
	m["env"] = envMap

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(p, append(b, '\n'), 0o600); err != nil {
		return err
	}

	if existingLaunchURL := getLaunchEnv("GOOGLE_GEMINI_BASE_URL"); existingLaunchURL != "" && !isGatewayURL(existingLaunchURL) {
		return fmt.Errorf("refusing to overwrite existing launchctl GOOGLE_GEMINI_BASE_URL (%s) not managed by amux", existingLaunchURL)
	}
	setLaunchEnv("GOOGLE_GEMINI_BASE_URL", baseURL)
	if existingKey := getLaunchEnv("GEMINI_API_KEY"); existingKey == "" || isAmuxOwnedEnv("GEMINI_API_KEY", existingKey) {
		setLaunchEnv("GEMINI_API_KEY", "amux-local")
	}
	return nil
}

// UnhookAgy restores native execution for Antigravity CLI.
func UnhookAgy() error {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
		if envMap, ok := m["env"].(map[string]any); ok {
			if val, exists := envMap["GOOGLE_GEMINI_BASE_URL"].(string); exists && isGatewayURL(val) {
				delete(envMap, "GOOGLE_GEMINI_BASE_URL")
				if k, ok := envMap["GEMINI_API_KEY"].(string); ok && isAmuxOwnedEnv("GEMINI_API_KEY", k) {
					delete(envMap, "GEMINI_API_KEY")
				}
				if mp, ok := m["modelProvider"].(string); ok && mp == "gemini" {
					delete(m, "modelProvider")
				}
			}
			if len(envMap) == 0 {
				delete(m, "env")
			} else {
				m["env"] = envMap
			}
		}
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicWriteFile(p, append(data, '\n'), 0o600); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if isGatewayURL(getLaunchEnv("GOOGLE_GEMINI_BASE_URL")) {
		unsetLaunchEnv("GOOGLE_GEMINI_BASE_URL")
		if isAmuxOwnedEnv("GEMINI_API_KEY", getLaunchEnv("GEMINI_API_KEY")) {
			unsetLaunchEnv("GEMINI_API_KEY")
		}
	}
	return nil
}

// IsAgyHooked checks if Antigravity CLI is currently hooked to the gateway.
func IsAgyHooked() (bool, string) {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if envMap, ok := m["env"].(map[string]any); ok {
				if val, ok := envMap["GOOGLE_GEMINI_BASE_URL"].(string); ok && isGatewayURL(val) {
					return true, val
				}
			}
		}
	}
	envVal := getLaunchEnv("GOOGLE_GEMINI_BASE_URL")
	if isGatewayURL(envVal) {
		return true, envVal
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
		return errors.Join(
			HookClaude(baseURL),
			HookCodex(baseURL),
			HookCursor(baseURL),
			HookAgy(baseURL),
		)
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
		return errors.Join(
			UnhookClaude(),
			UnhookCodex(),
			UnhookCursor(),
			UnhookAgy(),
		)
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
	var errs []error

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
					errs = append(errs, fmt.Errorf("hook claude: %w", err))
				}
			} else if hasAvail && hooked {
				// Quota reset or subscription available -> Auto-detach hook and restore direct Keychain!
				if err := UnhookClaude(); err != nil {
					errs = append(errs, fmt.Errorf("unhook claude: %w", err))
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					errs = append(errs, fmt.Errorf("sync claude keychain: %w", err))
				}
			}
		case "openai":
			hooked, _ := IsCodexHooked()
			if exhausted && !hooked {
				if err := HookCodex(""); err != nil {
					errs = append(errs, fmt.Errorf("hook codex: %w", err))
				}
			} else if hasAvail && hooked {
				if err := UnhookCodex(); err != nil {
					errs = append(errs, fmt.Errorf("unhook codex: %w", err))
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					errs = append(errs, fmt.Errorf("sync codex keychain: %w", err))
				}
			}
		case "gemini":
			hooked, _ := IsAgyHooked()
			if exhausted && !hooked {
				if err := HookAgy(""); err != nil {
					errs = append(errs, fmt.Errorf("hook agy: %w", err))
				}
			} else if hasAvail && hooked {
				if err := UnhookAgy(); err != nil {
					errs = append(errs, fmt.Errorf("unhook agy: %w", err))
				}
				if err := identity.SyncIdentityToNativeKeychain(available); err != nil {
					errs = append(errs, fmt.Errorf("sync agy keychain: %w", err))
				}
			}
		}
	}
	return errors.Join(errs...)
}
