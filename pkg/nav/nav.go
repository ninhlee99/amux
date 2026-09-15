package nav

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	FileMapMD      = "AI_CODEBASE_MAP.md"
	FileLocateYAML = "ai-locate.yaml"
	FileAgentsMD   = "AGENTS.md"
	FileModulesMD  = "MODULES.md"
	FileRootMeta   = "project_root.txt"
)

// Bundle describes where an AI agent should look for navigation in a client repo.
type Bundle struct {
	ProjectRoot string `json:"project_root"`
	Workspace   string `json:"workspace"` // folder name under ~/.am/workspaces/

	Label string `json:"label"`

	// Workspace paths (primary store)
	WorkspaceDir    string `json:"workspace_dir,omitempty"`
	WorkspaceMap    string `json:"workspace_map,omitempty"`
	WorkspaceLocate string `json:"workspace_locate,omitempty"`

	// Optional in-repo copies (legacy / preferred if present)
	MapPath    string `json:"map_path,omitempty"`
	LocatePath string `json:"locate_path,omitempty"`
	AgentsPath string `json:"agents_path,omitempty"`

	PrimaryMap    string `json:"primary_map,omitempty"`
	PrimaryLocate string `json:"primary_locate,omitempty"`
}

// PathsOnly is the safe agent-facing view: pointers only — never a full GRAPH dump.
type PathsOnly struct {
	ProjectRoot   string `json:"project_root"`
	Workspace     string `json:"workspace"`
	WorkspaceDir  string `json:"workspace_dir,omitempty"`
	PrimaryMap    string `json:"primary_map,omitempty"`
	PrimaryLocate string `json:"primary_locate,omitempty"`
	Hint          string `json:"hint"`
}

// AsPathsOnly returns navigation pointers + token-saving hint for agents.
func (b Bundle) AsPathsOnly() PathsOnly {
	return PathsOnly{
		ProjectRoot:   b.ProjectRoot,
		Workspace:     b.Workspace,
		WorkspaceDir:  b.WorkspaceDir,
		PrimaryMap:    b.PrimaryMap,
		PrimaryLocate: b.PrimaryLocate,
		Hint:          "am map recent → get/learn; am map graph <module> for 1 subnet. Never dump full GRAPH/MODULES into the LLM.",
	}
}

// GitRoot returns the git toplevel for start, or start if not in a repo.
func GitRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		start = wd
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	return gitRootOrSelf(abs)
}

// WorkspaceName is ~/.am/workspaces/<name>/ folder for this project root.
// Prefer clean project basename; on collision with another root, append -hash4.
func WorkspaceName(absRoot string) string {
	absRoot = filepath.Clean(absRoot)
	base := sanitizeSlug(filepath.Base(absRoot))
	if base == "" {
		base = "project"
	}
	candidate := filepath.Join(WorkspacesRoot(), base)
	if meta, err := os.ReadFile(filepath.Join(candidate, FileRootMeta)); err == nil {
		existing := strings.TrimSpace(string(meta))
		if existing == "" || existing == absRoot {
			return base
		}
		// Same name, different root → disambiguate.
		return base + "-" + shortHash(absRoot)
	}
	// Also claim if another dir already used base-hash for this root.
	hashed := base + "-" + shortHash(absRoot)
	if meta, err := os.ReadFile(filepath.Join(WorkspacesRoot(), hashed, FileRootMeta)); err == nil {
		if strings.TrimSpace(string(meta)) == absRoot {
			return hashed
		}
	}
	return base
}

func shortHash(absRoot string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(absRoot)))
	return hex.EncodeToString(sum[:4])
}

func sanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "project"
	}
	return out
}

// WorkspacesRoot is ~/.am/workspaces (or $AMUX_HOME/workspaces).
func WorkspacesRoot() string {
	return filepath.Join(amBaseDir(), "workspaces")
}

// WorkspaceDir returns ~/.am/workspaces/<name>/.
func WorkspaceDir(name string) string {
	return filepath.Join(WorkspacesRoot(), name)
}

func amBaseDir() string {
	if d := strings.TrimSpace(os.Getenv("AMUX_HOME")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".am"
	}
	return filepath.Join(home, ".am")
}

// Resolve finds navigation files for a client project (repo root).
// Priority: ~/.am/workspaces/<name>/ → repo docs/ → repo AGENTS.md.
func Resolve(projectRoot string) Bundle {
	root := GitRoot(projectRoot)
	if root == "" {
		return Bundle{}
	}
	abs, _ := filepath.Abs(root)
	name := WorkspaceName(abs)
	ws := WorkspaceDir(name)
	b := Bundle{
		ProjectRoot:     abs,
		Workspace:       name,
		Label:           filepath.Base(abs),
		WorkspaceDir:    ws,
		WorkspaceMap:    filepath.Join(ws, FileMapMD),
		WorkspaceLocate: filepath.Join(ws, FileLocateYAML),
	}

	docs := filepath.Join(abs, "docs")
	b.MapPath = filepath.Join(docs, FileMapMD)
	b.LocatePath = filepath.Join(docs, FileLocateYAML)
	b.AgentsPath = filepath.Join(abs, FileAgentsMD)

	// Workspace first — amux-owned store for proxy/client projects.
	b.PrimaryMap = firstExisting(b.WorkspaceMap, b.MapPath, b.AgentsPath)
	b.PrimaryLocate = firstExisting(b.WorkspaceLocate, b.LocatePath)
	return b
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// HasProjectMap reports whether the project has any navigation artifact.
func (b Bundle) HasProjectMap() bool {
	return b.PrimaryMap != "" || b.PrimaryLocate != ""
}

// IsAmuxRepository is true when this root is the amux tool repo itself.
func (b Bundle) IsAmuxRepository() bool {
	if st, err := os.Stat(filepath.Join(b.ProjectRoot, "go.mod")); err != nil || st.IsDir() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(b.ProjectRoot, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module amux-accounts")
}

// EnsureWorkspace writes project_root.txt and mirrors any in-repo docs into
// ~/.am/workspaces/<name>/.
func EnsureWorkspace(b Bundle) error {
	if b.Workspace == "" {
		return nil
	}
	dir := WorkspaceDir(b.Workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if fileExists(b.MapPath) && !fileExists(b.WorkspaceMap) {
		if err := copyFile(b.MapPath, b.WorkspaceMap); err != nil {
			return err
		}
	}
	if fileExists(b.LocatePath) && !fileExists(b.WorkspaceLocate) {
		if err := copyFile(b.LocatePath, b.WorkspaceLocate); err != nil {
			return err
		}
	}
	meta := filepath.Join(dir, FileRootMeta)
	return os.WriteFile(meta, []byte(b.ProjectRoot+"\n"), 0o644)
}

// EnsureStoreMirror is kept as an alias for older call sites.
func EnsureStoreMirror(b Bundle) error { return EnsureWorkspace(b) }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// WalkWorkspaces lists known workspace folders under ~/.am/workspaces/.
func WalkWorkspaces(fn func(name string, rootHint string) error) error {
	root := WorkspacesRoot()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() && path == root {
			return nil
		}
		if d.Name() == FileRootMeta {
			name := filepath.Base(filepath.Dir(path))
			hint, _ := os.ReadFile(path)
			return fn(name, strings.TrimSpace(string(hint)))
		}
		return nil
	})
}

// Deprecated aliases.
func StoreID(absRoot string) string      { return WorkspaceName(absRoot) }
func ProjectStoreDir(name string) string { return WorkspaceDir(name) }
func WalkStoreProjects(fn func(string, string) error) error {
	return WalkWorkspaces(fn)
}
