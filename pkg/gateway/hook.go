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

	"amux-accounts/pkg/env"
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
	// Placeholder credential: the gateway injects the real account token, so
	// Claude Code needs no login of its own and never reads the keychain.
	if cur, _ := envMap["ANTHROPIC_AUTH_TOKEN"].(string); cur == "" {
		envMap["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
	}
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
	if tok, _ := envMap["ANTHROPIC_AUTH_TOKEN"].(string); tok == "am-proxy" || tok == "amux-proxy" {
		delete(envMap, "ANTHROPIC_AUTH_TOKEN")
	}
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
	// No launchctl OPENAI_BASE_URL: that would reroute every app using the
	// OpenAI SDK, not just Codex. config.toml is what Codex reads.
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
				if len(m) == 0 {
					// Codex itself does not use config.json; amux created it.
					if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
						return err
					}
				} else {
					data, err := json.MarshalIndent(m, "", "  ")
					if err != nil {
						return err
					}
					if err := atomicWriteFile(p, append(data, '\n'), 0o600); err != nil {
						return err
					}
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
					// HookCodex separated its line from the first table with a
					// blank line; leave exactly one, as before the hook.
					if body := strings.TrimRight(rootSection, "\r\n"); body != "" {
						rootSection = body + "\n\n"
					} else {
						rootSection = ""
					}
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
					if strings.TrimSpace(tomlStr) == "" {
						// Only amux's line was there: amux created the file.
						if err := os.Remove(tomlPath); err != nil && !os.IsNotExist(err) {
							return err
						}
					} else if err := atomicWriteFile(tomlPath, []byte(tomlStr), 0o600); err != nil {
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

const agyShellBlockStart = "# >>> amux agy gateway >>>"
const agyShellBlockEnd = "# <<< amux agy gateway <<<"

func agyShellRCBlock(baseURL string) string {
	return fmt.Sprintf(`%s
if [ -f "$HOME/.gemini/antigravity-cli/settings.json" ] && grep -q '"modelProvider"[[:space:]]*:[[:space:]]*"gemini"' "$HOME/.gemini/antigravity-cli/settings.json" 2>/dev/null; then
  export GEMINI_API_KEY="${GEMINI_API_KEY:-amux-local}"
  export GOOGLE_GEMINI_BASE_URL="${GOOGLE_GEMINI_BASE_URL:-%s}"
fi
%s
`, agyShellBlockStart, baseURL, agyShellBlockEnd)
}

func targetShellRCs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
	}
	var res []string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			res = append(res, c)
		}
	}
	if len(res) == 0 {
		if strings.Contains(os.Getenv("SHELL"), "bash") {
			res = append(res, filepath.Join(home, ".bashrc"))
		} else {
			res = append(res, filepath.Join(home, ".zshrc"))
		}
	}
	return res
}

func syncAgyShellRC(baseURL string, install bool) error {
	for _, rc := range targetShellRCs() {
		b, err := os.ReadFile(rc)
		if err != nil && !os.IsNotExist(err) {
			continue
		}
		if err != nil && !install {
			continue // never create an rc file just to remove nothing from it
		}
		content := string(b)
		orig := content
		startIdx := strings.Index(content, agyShellBlockStart)
		endIdx := strings.Index(content, agyShellBlockEnd)
		if startIdx != -1 && endIdx != -1 && endIdx >= startIdx {
			endIdx += len(agyShellBlockEnd)
			if endIdx < len(content) && content[endIdx] == '\n' {
				endIdx++
			}
			content = content[:startIdx] + content[endIdx:]
		}

		if install {
			if content != "" && !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += agyShellRCBlock(baseURL)
		}
		if content == orig {
			continue
		}
		_ = writeUserFile(rc, []byte(content))
	}
	return nil
}

// writeUserFile rewrites a user-owned dotfile in place: it follows symlinks
// (dotfile managers) instead of replacing them, and keeps the file mode.
func writeUserFile(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	return atomicWriteFile(path, data, perm)
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
		setHookState("agy_model_provider", true)
	} else if _, known := hookState("agy_model_provider"); !known {
		setHookState("agy_model_provider", false) // the user's own value
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
	mEnv := env.LoadEnvVars()
	mEnv["GOOGLE_GEMINI_BASE_URL"] = baseURL
	mEnv["GEMINI_API_KEY"] = "amux-local"
	_ = env.SaveEnvVars(mEnv)
	_ = syncAgyShellRC(baseURL, true)
	return nil
}

// UnhookAgy restores native execution for Antigravity CLI. It removes only
// what HookAgy added: modelProvider is dropped only when amux set it.
func UnhookAgy() error {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", p, err)
			}
		}
		changed := false
		if envMap, ok := m["env"].(map[string]any); ok {
			if val, exists := envMap["GOOGLE_GEMINI_BASE_URL"].(string); exists && isGatewayURL(val) {
				delete(envMap, "GOOGLE_GEMINI_BASE_URL")
				if k, ok := envMap["GEMINI_API_KEY"].(string); ok && isAmuxOwnedEnv("GEMINI_API_KEY", k) {
					delete(envMap, "GEMINI_API_KEY")
				}
				// Older amux versions kept no record: they always set it.
				setByAmux, known := hookState("agy_model_provider")
				if mp, ok := m["modelProvider"].(string); ok && mp == "gemini" && (setByAmux || !known) {
					delete(m, "modelProvider")
				}
				if len(envMap) == 0 {
					delete(m, "env")
				} else {
					m["env"] = envMap
				}
				changed = true
			}
		}
		if changed {
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return err
			}
			if err := atomicWriteFile(p, append(data, '\n'), 0o600); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	clearHookState("agy_model_provider")
	if isGatewayURL(getLaunchEnv("GOOGLE_GEMINI_BASE_URL")) {
		unsetLaunchEnv("GOOGLE_GEMINI_BASE_URL")
		if isAmuxOwnedEnv("GEMINI_API_KEY", getLaunchEnv("GEMINI_API_KEY")) {
			unsetLaunchEnv("GEMINI_API_KEY")
		}
	}
	mEnv := env.LoadEnvVars()
	if _, ok := mEnv["GOOGLE_GEMINI_BASE_URL"]; ok {
		delete(mEnv, "GOOGLE_GEMINI_BASE_URL")
		delete(mEnv, "GEMINI_API_KEY")
		_ = env.SaveEnvVars(mEnv)
	}
	_ = syncAgyShellRC("", false)
	return nil
}

// IsAgyHooked checks if Antigravity CLI is currently hooked to the gateway.
func IsAgyHooked() (bool, string) {
	p := AGYSettingsPath()
	if b, err := os.ReadFile(p); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if mp, ok := m["modelProvider"].(string); ok && mp != "" {
				if envMap, ok := m["env"].(map[string]any); ok {
					if val, ok := envMap["GOOGLE_GEMINI_BASE_URL"].(string); ok && isGatewayURL(val) {
						return true, val
					}
				}
				envVal := getLaunchEnv("GOOGLE_GEMINI_BASE_URL")
				if isGatewayURL(envVal) {
					return true, envVal
				}
			}
		}
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
