package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ServerName is the key amux registers itself under in every MCP host.
const ServerName = "amux"

// Target is one MCP host whose config amux can register itself in.
type Target struct {
	Name  string // CLI name: claude, cursor, codex, …
	Label string
	// Path of the config file amux edits.
	Path func(home string) string
	// Format: "json" (object under Key) or "toml" (Codex).
	Format string
	Key    string
	// Entry builds the server entry for the amux binary.
	Entry func(bin string) any
	// CLI, when set, is tried first (e.g. `claude mcp add`); the file edit is
	// the fallback when the binary is missing.
	CLI func(bin string) (add, remove []string)
}

func appSupport(home, app string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", app)
	case "windows":
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, app)
		}
		return filepath.Join(home, "AppData", "Roaming", app)
	default:
		return filepath.Join(home, ".config", app)
	}
}

func stdioEntry(bin string) any {
	return map[string]any{"command": bin, "args": []string{"mcp"}}
}

// Targets lists every supported MCP host, in display order.
func Targets() []Target {
	return []Target{
		{
			Name: "claude", Label: "Claude Code", Format: "json", Key: "mcpServers",
			Path: func(h string) string { return filepath.Join(h, ".claude.json") },
			Entry: func(bin string) any {
				return map[string]any{"type": "stdio", "command": bin, "args": []string{"mcp"}, "env": map[string]any{}}
			},
			CLI: func(bin string) ([]string, []string) {
				return []string{"claude", "mcp", "add", "--scope", "user", ServerName, "--", bin, "mcp"},
					[]string{"claude", "mcp", "remove", "--scope", "user", ServerName}
			},
		},
		{
			Name: "claude-desktop", Label: "Claude Desktop", Format: "json", Key: "mcpServers",
			Path:  func(h string) string { return filepath.Join(appSupport(h, "Claude"), "claude_desktop_config.json") },
			Entry: stdioEntry,
		},
		{
			Name: "cursor", Label: "Cursor", Format: "json", Key: "mcpServers",
			Path:  func(h string) string { return filepath.Join(h, ".cursor", "mcp.json") },
			Entry: stdioEntry,
		},
		{
			Name: "windsurf", Label: "Windsurf", Format: "json", Key: "mcpServers",
			Path:  func(h string) string { return filepath.Join(h, ".codeium", "windsurf", "mcp_config.json") },
			Entry: stdioEntry,
		},
		{
			Name: "vscode", Label: "VS Code (Copilot agent)", Format: "json", Key: "servers",
			Path: func(h string) string { return filepath.Join(appSupport(h, "Code"), "User", "mcp.json") },
			Entry: func(bin string) any {
				return map[string]any{"type": "stdio", "command": bin, "args": []string{"mcp"}}
			},
		},
		{
			Name: "gemini", Label: "Gemini CLI", Format: "json", Key: "mcpServers",
			Path: func(h string) string { return filepath.Join(h, ".gemini", "settings.json") },
			Entry: func(bin string) any {
				return map[string]any{"command": bin, "args": []string{"mcp"}, "timeout": 600000}
			},
		},
		{
			Name: "agy", Label: "Antigravity", Format: "json", Key: "mcpServers",
			Path: func(h string) string {
				for _, rel := range []string{
					filepath.Join(".gemini", "config", "mcp_config.json"),
					filepath.Join(".gemini", "antigravity", "mcp_config.json"),
					filepath.Join(".gemini", "antigravity-cli", "mcp_config.json"),
				} {
					p := filepath.Join(h, rel)
					if _, err := os.Stat(p); err == nil {
						return p
					}
				}
				return filepath.Join(h, ".gemini", "antigravity", "mcp_config.json")
			},
			Entry: stdioEntry,
		},
		{
			Name: "codex", Label: "Codex CLI", Format: "toml",
			Path: func(h string) string { return filepath.Join(h, ".codex", "config.toml") },
		},
		{
			Name: "opencode", Label: "opencode", Format: "json", Key: "mcp",
			Path: func(h string) string { return filepath.Join(h, ".config", "opencode", "opencode.json") },
			Entry: func(bin string) any {
				return map[string]any{"type": "local", "command": []string{bin, "mcp"}, "enabled": true, "timeout": 600000}
			},
		},
		{
			Name: "zed", Label: "Zed", Format: "json", Key: "context_servers",
			Path: func(h string) string { return filepath.Join(h, ".config", "zed", "settings.json") },
			Entry: func(bin string) any {
				return map[string]any{"source": "custom", "command": bin, "args": []string{"mcp"}}
			},
		},
		{
			Name: "cline", Label: "Cline", Format: "json", Key: "mcpServers",
			Path: func(h string) string {
				return filepath.Join(appSupport(h, "Code"), "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
			},
			Entry: stdioEntry,
		},
		{
			Name: "roo", Label: "Roo Code", Format: "json", Key: "mcpServers",
			Path: func(h string) string {
				return filepath.Join(appSupport(h, "Code"), "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "cline_mcp_settings.json")
			},
			Entry: stdioEntry,
		},
	}
}

// FindTarget resolves a target by name (a few aliases accepted).
func FindTarget(name string) (Target, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	alias := map[string]string{
		"claude-code": "claude", "cc": "claude", "desktop": "claude-desktop",
		"code": "vscode", "copilot": "vscode", "antigravity": "agy",
		"gemini-cli": "gemini", "codex-cli": "codex",
		"roo-code": "roo", "roocode": "roo",
	}
	if a, ok := alias[n]; ok {
		n = a
	}
	for _, t := range Targets() {
		if t.Name == n {
			return t, true
		}
	}
	return Target{}, false
}

// Detected reports whether the host looks installed (its config dir exists).
func (t Target) Detected(home string) bool {
	if t.CLI != nil {
		add, _ := t.CLI("")
		if _, err := exec.LookPath(add[0]); err == nil {
			return true
		}
	}
	_, err := os.Stat(filepath.Dir(t.Path(home)))
	return err == nil
}

// Snippet is the config text a user can paste by hand.
func (t Target) Snippet(bin string) string {
	if t.Format == "toml" {
		return codexBlock(bin)
	}
	b, _ := json.MarshalIndent(map[string]any{t.Key: map[string]any{ServerName: t.Entry(bin)}}, "", "  ")
	return string(b)
}

// ErrHasComments means the file is JSONC; amux will not rewrite it and
// loses the user's comments — paste the snippet instead.
var ErrHasComments = errors.New("config file contains comments (JSONC); add the snippet by hand")

// Installed reports whether amux is registered in t's config file.
func (t Target) Installed(home string) bool {
	data, err := os.ReadFile(t.Path(home))
	if err != nil {
		return false
	}
	if t.Format == "toml" {
		return strings.Contains(string(data), "[mcp_servers."+ServerName+"]")
	}
	var doc map[string]any
	if json.Unmarshal(stripJSONComments(data), &doc) != nil {
		return false
	}
	m, _ := doc[t.Key].(map[string]any)
	_, ok := m[ServerName]
	return ok
}

// Install registers bin as the amux MCP server in t. It returns a short
// description of what was changed.
func (t Target) Install(home, bin string) (string, error) {
	if t.CLI != nil {
		add, remove := t.CLI(bin)
		if _, err := exec.LookPath(add[0]); err == nil {
			_ = exec.Command(remove[0], remove[1:]...).Run() // idempotent re-install
			if out, err := exec.Command(add[0], add[1:]...).CombinedOutput(); err != nil {
				return "", fmt.Errorf("%s: %v: %s", strings.Join(add[:3], " "), err, strings.TrimSpace(string(out)))
			}
			return "ran `" + strings.Join(add[:5], " ") + " amux`", nil
		}
	}
	path := t.Path(home)
	if t.Format == "toml" {
		return path, editFile(path, func(old []byte) ([]byte, error) {
			return []byte(setCodexBlock(string(old), codexBlock(bin))), nil
		})
	}
	return path, editJSON(path, func(doc map[string]any) {
		m, _ := doc[t.Key].(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		m[ServerName] = t.Entry(bin)
		doc[t.Key] = m
	})
}

// Uninstall removes the amux entry from t.
func (t Target) Uninstall(home string) (string, error) {
	if t.CLI != nil {
		_, remove := t.CLI("")
		if _, err := exec.LookPath(remove[0]); err == nil {
			if out, err := exec.Command(remove[0], remove[1:]...).CombinedOutput(); err != nil &&
				!strings.Contains(strings.ToLower(string(out)), "no ") {
				return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
			}
			return "ran `" + strings.Join(remove, " ") + "`", nil
		}
	}
	path := t.Path(home)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	}
	bak := path + ".amux.bak"
	_, bakErr := os.Stat(bak)
	createdByAmux := os.IsNotExist(bakErr) // install backs up any file that already had content
	var err error
	if t.Format == "toml" {
		err = rewriteFile(path, false, func(old []byte) ([]byte, error) {
			return []byte(setCodexBlock(string(old), "")), nil
		})
	} else {
		err = rewriteFile(path, false, jsonMutator(path, func(doc map[string]any) {
			if m, ok := doc[t.Key].(map[string]any); ok {
				delete(m, ServerName)
				if len(m) == 0 {
					delete(doc, t.Key)
				}
			}
		}))
	}
	if err != nil {
		return path, err
	}
	// Leave nothing behind: the install-time backup, and the file itself
	// when amux created it and it is now empty.
	_ = os.Remove(bak)
	if createdByAmux {
		if b, rerr := os.ReadFile(path); rerr == nil {
			if trimmed := strings.TrimSpace(string(b)); trimmed == "" || trimmed == "{}" {
				_ = os.Remove(path)
			}
		}
	}
	return path, nil
}

func codexBlock(bin string) string {
	q, _ := json.Marshal(bin) // TOML basic strings share JSON's escaping
	return fmt.Sprintf("[mcp_servers.%s]\ncommand = %s\nargs = [\"mcp\"]\nstartup_timeout_sec = 30\ntool_timeout_sec = 600\n", ServerName, q)
}

// setCodexBlock replaces (or removes, when block is "") the
// [mcp_servers.amux] table in a Codex config.toml, leaving the rest intact.
func setCodexBlock(content, block string) string {
	header := "[mcp_servers." + ServerName + "]"
	lines := strings.Split(content, "\n")
	var out []string
	skipping := false
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == header || strings.HasPrefix(trim, "[mcp_servers."+ServerName+".") {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(trim, "[") {
			skipping = false
		}
		if !skipping {
			out = append(out, ln)
		}
	}
	res := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if block != "" {
		if res != "" {
			res += "\n\n"
		}
		res += strings.TrimRight(block, "\n")
	}
	if res != "" {
		res += "\n"
	}
	return res
}

func editJSON(path string, mutate func(map[string]any)) error {
	return editFile(path, jsonMutator(path, mutate))
}

func jsonMutator(path string, mutate func(map[string]any)) func([]byte) ([]byte, error) {
	return func(old []byte) ([]byte, error) {
		doc := map[string]any{}
		if len(bytes.TrimSpace(old)) > 0 {
			stripped := stripJSONComments(old)
			if !bytes.Equal(bytes.TrimSpace(stripped), bytes.TrimSpace(old)) {
				return nil, ErrHasComments
			}
			if err := json.Unmarshal(old, &doc); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
		}
		mutate(doc)
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
}

// editFile rewrites path atomically, keeping a one-time .amux.bak of the
// original and its permissions.
func editFile(path string, mutate func([]byte) ([]byte, error)) error {
	return rewriteFile(path, true, mutate)
}

func rewriteFile(path string, backup bool, mutate func([]byte) ([]byte, error)) error {
	if real, err := filepath.EvalSymlinks(path); err == nil && real != "" {
		path = real
	}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	perm := os.FileMode(0o600)
	if st, serr := os.Stat(path); serr == nil {
		perm = st.Mode().Perm()
	}
	next, err := mutate(old)
	if err != nil {
		return err
	}
	if bytes.Equal(old, next) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if backup && len(old) > 0 {
		bak := path + ".amux.bak"
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			_ = os.WriteFile(bak, old, perm)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(next); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// stripJSONComments removes // and /* */ comments outside strings.
func stripJSONComments(b []byte) []byte {
	var out bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(b) && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
			if i < len(b) {
				out.WriteByte('\n')
			}
			continue
		}
		if c == '/' && i+1 < len(b) && b[i+1] == '*' {
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}

// TargetNames lists target names for help text.
func TargetNames() []string {
	var out []string
	for _, t := range Targets() {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}
