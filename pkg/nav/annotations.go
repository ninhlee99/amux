package nav

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const FileAnnotations = "annotations.json"

// FuncAnnotation is an AI/human refinement of a function after deeper reading.
// Survives am map update (re-scan overlays these summaries).
type FuncAnnotation struct {
	File      string    `json:"file"`                // repo-relative path
	Func      string    `json:"func"`                // function/type name
	Summary   string    `json:"summary"`             // improved description
	Notes     string    `json:"notes,omitempty"`     // optional longer note
	Source    string    `json:"source,omitempty"`    // agent | human | am-learn
	UpdatedAt time.Time `json:"updated_at"`
}

// AnnotationStore is persisted at ~/.am/workspaces/<name>/annotations.json.
type AnnotationStore struct {
	Version int              `json:"version"`
	Root    string           `json:"root,omitempty"`
	Items   []FuncAnnotation `json:"items"`
}

func annotationsPath(workspaceName string) string {
	return filepath.Join(WorkspaceDir(workspaceName), FileAnnotations)
}

// LoadAnnotations reads the annotation store (empty if missing).
func LoadAnnotations(workspaceName string) (AnnotationStore, error) {
	path := annotationsPath(workspaceName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AnnotationStore{Version: 1, Items: nil}, nil
		}
		return AnnotationStore{}, err
	}
	var s AnnotationStore
	if err := json.Unmarshal(data, &s); err != nil {
		return AnnotationStore{}, err
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s, nil
}

// SaveAnnotations writes the store atomically-ish.
func SaveAnnotations(workspaceName string, s AnnotationStore) error {
	if err := os.MkdirAll(WorkspaceDir(workspaceName), 0o755); err != nil {
		return err
	}
	s.Version = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(annotationsPath(workspaceName), append(data, '\n'), 0o644)
}

// LearnFuncs upserts annotations then regenerates map so MODULES.md reflects them.
func LearnFuncs(startDir string, anns []FuncAnnotation) (Bundle, int, error) {
	root := GitRoot(startDir)
	if root == "" {
		return Bundle{}, 0, fmt.Errorf("cannot resolve project directory")
	}
	b := Resolve(root)
	if err := os.MkdirAll(b.WorkspaceDir, 0o755); err != nil {
		return b, 0, err
	}
	// Ensure base map exists
	if !fileExists(b.WorkspaceMap) {
		if _, err := GenerateMap(root, true); err != nil {
			return b, 0, err
		}
		b = Resolve(root)
	}

	store, err := LoadAnnotations(b.Workspace)
	if err != nil {
		return b, 0, err
	}
	store.Root = b.ProjectRoot
	n := 0
	now := time.Now()
	for _, a := range anns {
		a.File = filepath.ToSlash(strings.TrimSpace(a.File))
		a.Func = strings.TrimSpace(a.Func)
		a.Summary = strings.TrimSpace(a.Summary)
		if a.File == "" || a.Func == "" || a.Summary == "" {
			continue
		}
		if a.Source == "" {
			a.Source = "agent"
		}
		a.UpdatedAt = now
		store.Items = upsertAnnotation(store.Items, a)
		n++
	}
	if n == 0 {
		return b, 0, fmt.Errorf("no valid annotations (need file, func, summary)")
	}
	if err := SaveAnnotations(b.Workspace, store); err != nil {
		return b, 0, err
	}
	// Rebuild map with overlays
	b, err = GenerateMap(root, true)
	return b, n, err
}

func upsertAnnotation(items []FuncAnnotation, a FuncAnnotation) []FuncAnnotation {
	keyFile := strings.ToLower(a.File)
	keyFunc := strings.ToLower(a.Func)
	for i := range items {
		if strings.ToLower(items[i].File) == keyFile && strings.ToLower(items[i].Func) == keyFunc {
			items[i] = a
			return items
		}
	}
	return append(items, a)
}

// ApplyAnnotations overlays learned summaries onto scan results.
func ApplyAnnotations(scan *ScanResult, store AnnotationStore) int {
	if scan == nil || len(store.Items) == 0 {
		return 0
	}
	idx := map[string]FuncAnnotation{}
	for _, a := range store.Items {
		k := annKey(a.File, a.Func)
		idx[k] = a
	}
	applied := 0
	for mi := range scan.Modules {
		for fi := range scan.Modules[mi].Files {
			f := &scan.Modules[mi].Files[fi]
			for gi := range f.Funcs {
				fn := &f.Funcs[gi]
				if a, ok := idx[annKey(f.RelPath, fn.Name)]; ok {
					fn.Summary = a.Summary
					if a.Notes != "" {
						fn.Summary = a.Summary + " · " + truncate(a.Notes, 120)
					}
					fn.Summary = "[learned] " + strings.TrimPrefix(fn.Summary, "[learned] ")
					applied++
				}
			}
		}
	}
	return applied
}

func annKey(file, fn string) string {
	return strings.ToLower(filepath.ToSlash(file)) + "::" + strings.ToLower(fn)
}
