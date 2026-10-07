package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The proxy is started on demand by a Claude Code lifecycle hook and then
// runs indefinitely so a still-open tab is never left pointed at a dead
// port. `am hook install` wires:
//
//   SessionStart -> am proxy up     (spawn if needed, register the session)
//   SessionEnd   -> am proxy down   (deregister; just updates `am status`'s count)
//   Stop         -> am proxy down   (extra safety net if SessionEnd is missed)
//
// It also needs ANTHROPIC_BASE_URL to point `claude` at the proxy. Hooks
// can't set session env, so `am hook install` offers to append one line to
// the shell rc: `eval "$(am env)"`, which resolves ANTHROPIC_BASE_URL (and
// any vars from `am env set`) fresh in every new shell — see pkg/env.

const HookTag = "am proxy" // how we recognise our own entries

func ClaudeAvailable() bool {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func GeminiAvailable() bool {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".gemini")); err == nil {
		return true
	}
	if _, err := os.Stat("/Applications/Antigravity.app"); err == nil {
		return true
	}
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, "Applications", "Antigravity.app")); err == nil {
			return true
		}
	}
	if _, err := exec.LookPath("agy"); err == nil {
		return true
	}
	_, err := exec.LookPath("antigravity")
	return err == nil
}

func CodexAvailable() bool {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		return true
	}
	_, err := exec.LookPath("codex")
	return err == nil
}

func CursorAvailable() bool {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err == nil {
		return true
	}
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		return true
	}
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, "Applications", "Cursor.app")); err == nil {
			return true
		}
	}
	_, err := exec.LookPath("cursor")
	return err == nil
}

func WindsurfAvailable() bool {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".codeium", "windsurf")); err == nil {
		return true
	}
	if _, err := os.Stat("/Applications/Windsurf.app"); err == nil {
		return true
	}
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, "Applications", "Windsurf.app")); err == nil {
			return true
		}
	}
	_, err := exec.LookPath("windsurf")
	return err == nil
}

func ClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func LoadClaudeSettings() map[string]any {
	b, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveClaudeSettings(m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(ClaudeSettingsPath()), 0o755); err != nil {
		return fmt.Errorf("mkdir .claude: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(ClaudeSettingsPath(), append(b, '\n'), 0o600)
}

// AddHook appends our command to settings["hooks"][event], leaving any other
// entries (and other events) untouched. A previous version of our own entry
// (however it was formatted) is dropped first so re-running `install` never
// piles up duplicates.
func AddHook(m map[string]any, event, command string) {
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		m["hooks"] = hooks
	}
	list, _ := hooks[event].([]any)
	var kept []any
	for _, e := range list {
		if !HookEntryIsOurs(e) {
			kept = append(kept, e)
		}
	}
	entry := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
	}
	hooks[event] = append(kept, entry)
}

// HookEntryIsOurs recognises our own hook entries even across the command
// formats we've used historically: a bare "am proxy up"/"am proxy down", or
// (older installs) a quoted absolute path to the binary — `"<path>/am"
// proxy up` — which doesn't contain the literal "am proxy" substring
// because of the closing quote, hence the extra checks.
func isOurHookCommand(c string) bool {
	return strings.Contains(c, HookTag) ||
		strings.Contains(c, "proxy up") ||
		strings.Contains(c, "proxy down") ||
		strings.Contains(c, "hook claude") ||
		strings.Contains(c, "hook agy") ||
		strings.Contains(c, "hook codex") ||
		strings.Contains(c, "hook cursor")
}

func HookEntryIsOurs(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	if hooks, ok := m["hooks"].([]any); ok {
		for _, h := range hooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			c, _ := hm["command"].(string)
			if isOurHookCommand(c) {
				return true
			}
		}
	}
	if c, ok := m["command"].(string); ok {
		if isOurHookCommand(c) {
			return true
		}
	}
	return false
}

func HookUninstall() (int, error) {
	m := LoadClaudeSettings()
	hooks, _ := m["hooks"].(map[string]any)
	n := 0
	if hooks != nil {
		for event, listRaw := range hooks {
			list, ok := listRaw.([]any)
			if !ok {
				continue
			}
			var kept []any
			for _, e := range list {
				if HookEntryIsOurs(e) {
					n++
				} else {
					kept = append(kept, e)
				}
			}
			if len(kept) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = kept
			}
		}
		if len(hooks) == 0 {
			delete(m, "hooks")
		}
	}
	if IsOurStatusLine(m) {
		delete(m, "statusLine")
		n++
	}
	if env, ok := m["env"].(map[string]any); ok {
		n += removeAmuxClaudeEnv(env)
		if len(env) == 0 {
			delete(m, "env")
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, SaveClaudeSettings(m)
}

// removeAmuxClaudeEnv deletes only the env values amux wrote: a local
// gateway base URL, the "am-proxy" placeholder token, and the model amux
// pinned alongside them. Values the user set themselves are kept.
func removeAmuxClaudeEnv(env map[string]any) int {
	n := 0
	base, _ := env["ANTHROPIC_BASE_URL"].(string)
	tok, _ := env["ANTHROPIC_AUTH_TOKEN"].(string)
	ownBase := isLocalGatewayURL(base)
	ownTok := tok == "am-proxy" || tok == "amux-proxy"
	if ownBase {
		delete(env, "ANTHROPIC_BASE_URL")
		n++
	}
	if ownTok {
		delete(env, "ANTHROPIC_AUTH_TOKEN")
		n++
	}
	if model, _ := env["ANTHROPIC_MODEL"].(string); ownBase && ownTok && strings.Contains(model, "sonnet") {
		delete(env, "ANTHROPIC_MODEL")
		n++
	}
	return n
}

// isLocalGatewayURL reports whether u points at a gateway on this machine.
func isLocalGatewayURL(u string) bool {
	u = strings.TrimSpace(u)
	for _, p := range []string{"http://127.0.0.1:", "http://localhost:", "http://0.0.0.0:"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

// InstallStatusLine writes Claude Code / AGY statusLine command.
// Overwrites our prior install and caveman-only badges (am statusline
// re-embeds the caveman badge). Leaves other custom statuslines alone.
func InstallStatusLine(m map[string]any, command string) {
	if m == nil {
		return
	}
	if existing, ok := m["statusLine"].(map[string]any); ok {
		cmd, _ := existing["command"].(string)
		if cmd != "" && !statusLineCommandIsOurs(cmd) && !statusLineCommandIsCaveman(cmd) {
			return
		}
	}
	m["statusLine"] = map[string]any{
		"type":    "command",
		"command": command,
	}
}

func IsOurStatusLine(m map[string]any) bool {
	sl, _ := m["statusLine"].(map[string]any)
	if sl == nil {
		return false
	}
	cmd, _ := sl["command"].(string)
	return statusLineCommandIsOurs(cmd)
}

func statusLineCommandIsOurs(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	return strings.HasSuffix(cmd, " statusline") || strings.Contains(cmd, "\" statusline")
}

func statusLineCommandIsCaveman(cmd string) bool {
	low := strings.ToLower(cmd)
	return strings.Contains(low, "caveman-statusline")
}

// claudeSettingsEnvKeys are the vars we own inside settings["env"] — never
// touch anything else a user put there themselves.
var claudeSettingsEnvKeys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"}

// SyncClaudeSettingsEnv mirrors the proxy's reachability into
// ~/.claude/settings.json's "env" block, which Claude Code reads at the
// start of every new session — independent of shell rc (`eval "$(am env)"`)
// and launchctl (SyncLaunchctlEnv), both of which only reach processes
// spawned *after* the change. A terminal/session already running when the
// proxy goes up or down won't see this either, but every session opened
// from that point on will, without the user needing to `eval` anything.
//
// proxyUp true  -> set ANTHROPIC_BASE_URL/ANTHROPIC_AUTH_TOKEN to proxyBase
//
//	and default ANTHROPIC_MODEL to the current Sonnet (claude-sonnet-5-5).
//
// proxyUp false -> remove keys so a new session falls through to the
// real Anthropic API on whatever ANTHROPIC_API_KEY / subscription login it
// already has.
func SyncClaudeSettingsEnv(proxyUp bool, proxyBase string) error {
	m := LoadClaudeSettings()
	env, _ := m["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	if proxyUp {
		env["ANTHROPIC_BASE_URL"] = proxyBase
		env["ANTHROPIC_AUTH_TOKEN"] = "am-proxy"
		env["ANTHROPIC_MODEL"] = "claude-sonnet-5-5"
	} else {
		for _, k := range claudeSettingsEnvKeys {
			delete(env, k)
		}
	}
	if len(env) == 0 {
		delete(m, "env")
	} else {
		m["env"] = env
	}
	return SaveClaudeSettings(m)
}

// SyncCodexSettingsEnv upserts openai_base_url into ~/.codex/config.toml so
// Codex CLI sessions opened after proxy up/down hit the gateway without
// relying solely on launchctl/shell env.
func SyncCodexSettingsEnv(proxyUp bool, proxyBase string) error {
	path := CodexConfigPath()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(b)
	next, changed := syncCodexBaseURL(content, proxyUp, proxyBase)
	if !changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(next), 0o600)
}

var (
	reCodexOpenAIBase = regexp.MustCompile(`(?m)^\s*openai_base_url\s*=\s*.*$`)
	reCodexOpenAIKey  = regexp.MustCompile(`(?m)^\s*openai_api_key\s*=\s*"am-proxy".*$`)
)

func syncCodexBaseURL(content string, proxyUp bool, proxyBase string) (string, bool) {
	changed := false
	if proxyUp {
		base := strings.TrimRight(proxyBase, "/") + "/v1"
		line := fmt.Sprintf(`openai_base_url = %q`, base)
		keyLine := `openai_api_key = "am-proxy"`
		if loc := reCodexOpenAIBase.FindStringIndex(content); loc != nil {
			if strings.TrimSpace(content[loc[0]:loc[1]]) != line {
				content = content[:loc[0]] + line + content[loc[1]:]
				changed = true
			}
		} else {
			content = strings.TrimRight(content, "\n")
			if content != "" {
				content += "\n"
			}
			content += line + "\n"
			changed = true
		}
		if !reCodexOpenAIKey.MatchString(content) {
			if loc := reCodexOpenAIBase.FindStringIndex(content); loc != nil {
				insertAt := loc[1]
				content = content[:insertAt] + "\n" + keyLine + content[insertAt:]
				changed = true
			}
		}
		return content, changed
	}
	if loc := reCodexOpenAIBase.FindStringIndex(content); loc != nil {
		content = content[:loc[0]] + content[loc[1]:]
		changed = true
	}
	if loc := reCodexOpenAIKey.FindStringIndex(content); loc != nil {
		content = content[:loc[0]] + content[loc[1]:]
		changed = true
	}
	if changed {
		content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	}
	return content, changed
}

// SyncClientSettingsEnv mirrors proxy reachability into Claude + Codex + Cursor client configs.
func SyncClientSettingsEnv(proxyUp bool, proxyBase string) error {
	var first error
	if err := SyncClaudeSettingsEnv(proxyUp, proxyBase); err != nil && first == nil {
		first = err
	}
	if err := SyncCodexSettingsEnv(proxyUp, proxyBase); err != nil && first == nil {
		first = err
	}
	if err := SyncCursorSettingsEnv(proxyUp, proxyBase); err != nil && first == nil {
		first = err
	}
	return first
}

// ClientSettingsPointToProxy reports whether Claude Code, Codex, or Cursor config files
// currently point at the proxy gateway.
func ClientSettingsPointToProxy() bool {
	m := LoadClaudeSettings()
	if env, ok := m["env"].(map[string]any); ok {
		if val, ok := env["ANTHROPIC_BASE_URL"].(string); ok && strings.TrimSpace(val) != "" {
			return true
		}
	}
	path := CodexConfigPath()
	if b, err := os.ReadFile(path); err == nil {
		if reCodexOpenAIBase.Match(b) {
			return true
		}
	}
	cm := LoadCursorSettings()
	if base, ok := cm["cursor.general.openaiBaseUrl"].(string); ok && strings.TrimSpace(base) != "" {
		return true
	}
	if base, ok := cm["openai.baseUrl"].(string); ok && strings.TrimSpace(base) != "" {
		return true
	}
	return false
}

// ShellRC returns the user's shell rc file for zsh/bash, or "" if the shell
// isn't one we know how to wire automatically.
func ShellRC() string {
	home, _ := os.UserHomeDir()
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	default:
		return ""
	}
}

// RCHasLine reports whether path already contains line.
func RCHasLine(path, line string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), line)
}

// AppendLine appends text to path, creating it if necessary.
func AppendLine(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

func GeminiConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "config")
}

func GeminiHooksPath() string {
	return filepath.Join(GeminiConfigDir(), "hooks.json")
}

func LoadGeminiHooks() map[string]any {
	b, err := os.ReadFile(GeminiHooksPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveGeminiHooks(m map[string]any) error {
	p := GeminiHooksPath()
	if len(m) == 0 {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir .gemini/config: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func GeminiHookInstall() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	m := LoadGeminiHooks()
	m["amux-proxy"] = map[string]any{
		"SessionStart": []any{
			map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%q hook agy start", self),
				"timeout": 15,
			},
		},
		"PreInvocation": []any{
			map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%q hook agy start", self),
				"timeout": 15,
			},
		},
		"Stop": []any{
			map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%q hook agy stop", self),
				"timeout": 15,
			},
		},
	}
	if err := SaveGeminiHooks(m); err != nil {
		return err
	}
	return installAGYStatusLine(self)
}

func GeminiHookUninstall() (int, error) {
	n := 0
	m := LoadGeminiHooks()
	if _, ok := m["amux-proxy"]; ok {
		delete(m, "amux-proxy")
		if err := SaveGeminiHooks(m); err != nil {
			return n, err
		}
		n++
	}
	k, err := uninstallAGYStatusLine()
	return n + k, err
}

func GeminiHookInstalled() bool {
	m := LoadGeminiHooks()
	_, ok := m["amux-proxy"]
	return ok
}

func GeminiInstalledEvents() []string {
	m := LoadGeminiHooks()
	spec, ok := m["amux-proxy"].(map[string]any)
	if !ok {
		return nil
	}
	var evs []string
	for ev := range spec {
		evs = append(evs, ev)
	}
	sort.Strings(evs)
	return evs
}

func CodexHooksPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "hooks.json")
}

func LoadCodexHooks() map[string]any {
	b, err := os.ReadFile(CodexHooksPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveCodexHooks(m map[string]any) error {
	p := CodexHooksPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir .codex: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func CodexHookInstall() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".codex")); err != nil {
		return nil
	}
	m := LoadCodexHooks()
	AddHook(m, "SessionStart", fmt.Sprintf("%q hook codex start", self))
	AddHook(m, "SessionEnd", fmt.Sprintf("%q hook codex stop", self))
	AddHook(m, "Stop", fmt.Sprintf("%q hook codex stop", self))
	if err := SaveCodexHooks(m); err != nil {
		return err
	}
	return installCodexStatusLine()
}

func CodexHookUninstall() (int, error) {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".codex")); err != nil {
		return 0, nil
	}
	m := LoadCodexHooks()
	hooks, _ := m["hooks"].(map[string]any)
	n := 0
	if hooks != nil {
		for event, listRaw := range hooks {
			list, ok := listRaw.([]any)
			if !ok {
				continue
			}
			var kept []any
			for _, e := range list {
				if HookEntryIsOurs(e) {
					n++
				} else {
					kept = append(kept, e)
				}
			}
			if len(kept) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = kept
			}
		}
	}
	if n > 0 {
		if err := saveOrRemoveHooks(CodexHooksPath(), m, SaveCodexHooks); err != nil {
			return n, err
		}
	}
	_ = uninstallCodexStatusLine()
	return n, nil
}

func CodexHookInstalled() bool {
	m := LoadCodexHooks()
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	start, _ := hooks["SessionStart"].([]any)
	for _, e := range start {
		if HookEntryIsOurs(e) {
			return true
		}
	}
	return false
}

func CursorHooksPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor", "hooks.json")
}

func LoadCursorHooks() map[string]any {
	b, err := os.ReadFile(CursorHooksPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveCursorHooks(m map[string]any) error {
	p := CursorHooksPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir .cursor: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func CursorHookInstall() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err != nil {
		return nil
	}
	m := LoadCursorHooks()
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		m["hooks"] = hooks
	}
	if _, ok := m["version"]; !ok {
		m["version"] = 1
	}
	list, _ := hooks["sessionStart"].([]any)
	var kept []any
	for _, e := range list {
		if em, ok := e.(map[string]any); ok {
			if c, _ := em["command"].(string); isOurHookCommand(c) {
				continue
			}
		}
		kept = append(kept, e)
	}
	entry := map[string]any{"command": fmt.Sprintf("%q hook cursor start", self)}
	hooks["sessionStart"] = append(kept, entry)
	return SaveCursorHooks(m)
}

func CursorHookUninstall() (int, error) {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err != nil {
		return 0, nil
	}
	m := LoadCursorHooks()
	hooks, _ := m["hooks"].(map[string]any)
	n := 0
	if hooks != nil {
		list, _ := hooks["sessionStart"].([]any)
		var kept []any
		for _, e := range list {
			if em, ok := e.(map[string]any); ok {
				if c, _ := em["command"].(string); isOurHookCommand(c) {
					n++
					continue
				}
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(hooks, "sessionStart")
		} else {
			hooks["sessionStart"] = kept
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, saveOrRemoveHooks(CursorHooksPath(), m, SaveCursorHooks)
}

// saveOrRemoveHooks writes m back, or deletes the file when amux's entries
// were all it held.
func saveOrRemoveHooks(path string, m map[string]any, save func(map[string]any) error) error {
	if hooks, ok := m["hooks"].(map[string]any); ok && len(hooks) == 0 {
		delete(m, "hooks")
	}
	if len(m) == 0 || (len(m) == 1 && m["version"] != nil) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return save(m)
}

func CursorHookInstalled() bool {
	m := LoadCursorHooks()
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	list, _ := hooks["sessionStart"].([]any)
	for _, e := range list {
		if em, ok := e.(map[string]any); ok {
			if c, _ := em["command"].(string); isOurHookCommand(c) {
				return true
			}
		}
	}
	return false
}

func CursorSettingsPath() string {
	home, _ := os.UserHomeDir()
	macPath := filepath.Join(home, "Library", "Application Support", "Cursor", "User", "settings.json")
	if _, err := os.Stat(filepath.Dir(macPath)); err == nil {
		return macPath
	}
	linuxPath := filepath.Join(home, ".config", "Cursor", "User", "settings.json")
	if _, err := os.Stat(filepath.Dir(linuxPath)); err == nil {
		return linuxPath
	}
	return filepath.Join(home, ".cursor", "settings.json")
}

func LoadCursorSettings() map[string]any {
	p := CursorSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveCursorSettings(m map[string]any) error {
	p := CursorSettingsPath()
	if len(m) == 0 {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir cursor settings: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func SyncCursorSettingsEnv(proxyUp bool, proxyBase string) error {
	m := LoadCursorSettings()
	if proxyUp {
		base := strings.TrimRight(proxyBase, "/") + "/v1"
		m["cursor.general.openaiBaseUrl"] = base
		m["openai.baseUrl"] = base
		m["openai.apiKey"] = "am-proxy"
	} else {
		delete(m, "cursor.general.openaiBaseUrl")
		delete(m, "openai.baseUrl")
		if k, ok := m["openai.apiKey"].(string); ok && k == "am-proxy" {
			delete(m, "openai.apiKey")
		}
	}
	return SaveCursorSettings(m)
}

func WindsurfSettingsPath() string {
	home, _ := os.UserHomeDir()
	macPath := filepath.Join(home, "Library", "Application Support", "Windsurf", "User", "settings.json")
	if _, err := os.Stat(filepath.Dir(macPath)); err == nil {
		return macPath
	}
	linuxPath := filepath.Join(home, ".config", "Windsurf", "User", "settings.json")
	if _, err := os.Stat(filepath.Dir(linuxPath)); err == nil {
		return linuxPath
	}
	return macPath
}

func LoadWindsurfSettings() map[string]any {
	p := WindsurfSettingsPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func SaveWindsurfSettings(m map[string]any) error {
	p := WindsurfSettingsPath()
	if len(m) == 0 {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir windsurf settings: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func SyncWindsurfSettingsEnv(proxyUp bool, proxyBase string) error {
	m := LoadWindsurfSettings()
	if proxyUp {
		base := strings.TrimRight(proxyBase, "/") + "/v1"
		m["openai.baseUrl"] = base
		m["openai.apiKey"] = "am-proxy"
	} else {
		delete(m, "openai.baseUrl")
		if k, ok := m["openai.apiKey"].(string); ok && k == "am-proxy" {
			delete(m, "openai.apiKey")
		}
	}
	return SaveWindsurfSettings(m)
}

// UninstallAllHooks removes hooks across Claude, Gemini, Codex, and Cursor.
func UninstallAllHooks() error {
	_, _ = HookUninstall()
	_, _ = GeminiHookUninstall()
	_, _ = CodexHookUninstall()
	_, _ = CursorHookUninstall()
	return nil
}
