package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const codexStatusLineValue = `["used-tokens", "five-hour-limit", "weekly-limit"]`

var reCodexStatusLine = regexp.MustCompile(`(?m)^[ \t]*status_line[ \t]*=[ \t]*\[[^\]]*\][ \t]*`)

func AGYSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
}

func LoadAGYSettings() map[string]any {
	b, err := os.ReadFile(AGYSettingsPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveAGYSettings(m map[string]any) error {
	p := AGYSettingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir antigravity-cli: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func installAGYStatusLine(self string) error {
	m := LoadAGYSettings()
	InstallStatusLine(m, fmt.Sprintf("%q statusline", self))
	return SaveAGYSettings(m)
}

func uninstallAGYStatusLine() (int, error) {
	m := LoadAGYSettings()
	if !IsOurStatusLine(m) {
		return 0, nil
	}
	delete(m, "statusLine")
	return 1, SaveAGYSettings(m)
}

func CodexConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.toml")
}

func installCodexStatusLine() error {
	path := CodexConfigPath()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next, changed := ensureCodexStatusLine(string(b))
	if !changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(next), 0o600)
}

func uninstallCodexStatusLine() error {
	path := CodexConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	next, changed := removeOurCodexStatusLine(string(b))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o600)
}

func ensureCodexStatusLine(content string) (string, bool) {
	line := "status_line = " + codexStatusLineValue
	if loc := reCodexStatusLine.FindStringIndex(content); loc != nil {
		cur := strings.TrimSpace(content[loc[0]:loc[1]])
		if strings.Contains(cur, "used-tokens") && strings.Contains(cur, "five-hour-limit") && strings.Contains(cur, "weekly-limit") {
			return content, false
		}
		if isOurCodexStatusLine(cur) {
			return content[:loc[0]] + line + content[loc[1]:], true
		}
		return content, false
	}
	if strings.Contains(content, "[tui]") {
		return strings.Replace(content, "[tui]", "[tui]\n"+line, 1), true
	}
	out := strings.TrimRight(content, "\n")
	if out != "" {
		out += "\n\n"
	}
	return out + "[tui]\n" + line + "\n", true
}

func isOurCodexStatusLine(line string) bool {
	if !strings.Contains(line, "five-hour-limit") || !strings.Contains(line, "weekly-limit") {
		return false
	}
	for _, extra := range []string{"git", "model", "context", "branch", "dir"} {
		if strings.Contains(strings.ToLower(line), extra) {
			return false
		}
	}
	return true
}

func removeOurCodexStatusLine(content string) (string, bool) {
	loc := reCodexStatusLine.FindStringIndex(content)
	if loc == nil {
		return content, false
	}
	line := strings.TrimSpace(content[loc[0]:loc[1]])
	if !isOurCodexStatusLine(line) {
		return content, false
	}
	next := content[:loc[0]] + content[loc[1]:]
	return next, true
}
