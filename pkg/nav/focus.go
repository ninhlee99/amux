package nav

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const FileFocus = "focus.json"

// FocusState remembers files/funcs the agent already opened this session.
// Cheap machine state — not sent to LLM unless agent asks.
type FocusState struct {
	Version   int            `json:"version"`
	Root      string         `json:"root,omitempty"`
	Files     []string       `json:"files,omitempty"`
	Funcs     []FocusFuncRef `json:"funcs,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type FocusFuncRef struct {
	File string `json:"file"`
	Func string `json:"func"`
}

// FocusHit is one function in the narrow check/update set.
type FocusHit struct {
	File       string `json:"file"`
	Func       string `json:"func"`
	Kind       string `json:"kind,omitempty"`
	Line       int    `json:"line,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Learned    bool   `json:"learned"`
	Reason     string `json:"reason"` // git | focus | both
	NeedsLearn bool   `json:"needs_learn"`
}

// FocusReport is the only payload agents should read when checking/updating map.
type FocusReport struct {
	Root  string     `json:"root"`
	Files []string   `json:"files"`
	Funcs []FocusHit `json:"funcs"`
	Count int        `json:"count"`
	Since string     `json:"since,omitempty"`
}

// LoadFocus / SaveFocus persist session touch list under workspace.
func LoadFocus(workspaceName string) (FocusState, error) {
	path := filepath.Join(WorkspaceDir(workspaceName), FileFocus)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FocusState{Version: 1}, nil
		}
		return FocusState{}, err
	}
	var s FocusState
	if err := json.Unmarshal(data, &s); err != nil {
		return FocusState{}, err
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s, nil
}

func SaveFocus(workspaceName string, s FocusState) error {
	if err := os.MkdirAll(WorkspaceDir(workspaceName), 0o755); err != nil {
		return err
	}
	s.Version = 1
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(WorkspaceDir(workspaceName), FileFocus), append(data, '\n'), 0o644)
}

// NoteTouch records that the agent opened these files/funcs (no LLM, no full map).
func NoteTouch(startDir string, files []string, funcs []FocusFuncRef) (FocusState, error) {
	root := GitRoot(startDir)
	if root == "" {
		return FocusState{}, fmt.Errorf("cannot resolve project directory")
	}
	b := Resolve(root)
	s, err := LoadFocus(b.Workspace)
	if err != nil {
		return FocusState{}, err
	}
	s.Root = b.ProjectRoot
	for _, f := range files {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		s.Files = appendUnique(s.Files, f)
	}
	for _, fr := range funcs {
		fr.File = filepath.ToSlash(strings.TrimSpace(fr.File))
		fr.Func = strings.TrimSpace(fr.Func)
		if fr.File == "" || fr.Func == "" {
			continue
		}
		s.Funcs = upsertFocusFunc(s.Funcs, fr)
		s.Files = appendUnique(s.Files, fr.File)
	}
	if err := SaveFocus(b.Workspace, s); err != nil {
		return s, err
	}
	return s, nil
}

func upsertFocusFunc(items []FocusFuncRef, a FocusFuncRef) []FocusFuncRef {
	for i := range items {
		if strings.EqualFold(items[i].File, a.File) && strings.EqualFold(items[i].Func, a.Func) {
			items[i] = a
			return items
		}
	}
	return append(items, a)
}

// RecentFocus builds a narrow report: git-changed funcs ∪ session focus.
// Does NOT walk whole MODULES.md / whole repo into the agent context —
// machine filters first; print only this list.
func RecentFocus(startDir string, since string, needsLearnOnly bool) (FocusReport, error) {
	root := GitRoot(startDir)
	if root == "" {
		return FocusReport{}, fmt.Errorf("cannot resolve project directory")
	}
	b := Resolve(root)
	if since == "" {
		since = "HEAD"
	}

	changedFiles, err := gitChangedSourceFiles(root, since)
	if err != nil {
		changedFiles = nil
	}
	focus, _ := LoadFocus(b.Workspace)
	store, _ := LoadAnnotations(b.Workspace)

	fileSet := map[string]string{} // path → reason
	for _, f := range changedFiles {
		fileSet[f] = "git"
	}
	for _, f := range focus.Files {
		if _, ok := fileSet[f]; ok {
			fileSet[f] = "both"
		} else {
			fileSet[f] = "focus"
		}
	}

	// Also include files from focus funcs
	for _, fr := range focus.Funcs {
		if _, ok := fileSet[fr.File]; ok {
			if fileSet[fr.File] == "git" {
				fileSet[fr.File] = "both"
			}
		} else {
			fileSet[fr.File] = "focus"
		}
	}

	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)

	hunks, _ := gitDiffHunks(root, since)
	annIdx := map[string]FuncAnnotation{}
	for _, a := range store.Items {
		annIdx[annKey(a.File, a.Func)] = a
	}
	focusFuncWant := map[string]bool{}
	for _, fr := range focus.Funcs {
		focusFuncWant[annKey(fr.File, fr.Func)] = true
	}

	var hits []FocusHit
	for _, rel := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		funcs := extractFuncHits(string(data), rel)
		ranges := hunks[rel]
		reasonFile := fileSet[rel]
		for _, fn := range funcs {
			inDiff := lineInRanges(fn.Line, ranges)
			inFocus := focusFuncWant[annKey(rel, fn.Name)]
			// Narrow: only funcs that appear in git hunks OR were explicitly touched.
			// If file is new/untracked with no hunks, include all funcs in that file once.
			include := inFocus || inDiff || (reasonFile != "focus" && len(ranges) == 0)
			if !include {
				continue
			}
			reason := reasonFile
			if inDiff && inFocus {
				reason = "both"
			} else if inDiff {
				reason = "git"
			} else if inFocus {
				reason = "focus"
			}
			sum := fn.Summary
			learned := false
			if a, ok := annIdx[annKey(rel, fn.Name)]; ok {
				sum = "[learned] " + strings.TrimPrefix(a.Summary, "[learned] ")
				learned = true
			}
			needs := !learned
			if a, ok := annIdx[annKey(rel, fn.Name)]; ok && inDiff {
				// re-learn if annotation older than... we only know UpdatedAt; if inDiff, flag stale
				if !a.UpdatedAt.IsZero() {
					// always suggest re-check when lines changed
					needs = true
					sum = sum + " (stale vs git)"
				}
			}
			hit := FocusHit{
				File:       rel,
				Func:       fn.Name,
				Kind:       fn.Kind,
				Line:       fn.Line,
				Summary:    sum,
				Learned:    learned && !strings.Contains(sum, "stale"),
				Reason:     reason,
				NeedsLearn: needs,
			}
			if needsLearnOnly && !hit.NeedsLearn {
				continue
			}
			hits = append(hits, hit)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return FocusReport{
		Root:  root,
		Files: files,
		Funcs: hits,
		Count: len(hits),
		Since: since,
	}, nil
}

// LookupFunc returns one function summary from disk scan + annotations (no full map dump).
func LookupFunc(startDir, file, name string) (FocusHit, error) {
	root := GitRoot(startDir)
	if root == "" {
		return FocusHit{}, fmt.Errorf("cannot resolve project directory")
	}
	file = filepath.ToSlash(strings.TrimSpace(file))
	name = strings.TrimSpace(name)
	if file == "" || name == "" {
		return FocusHit{}, fmt.Errorf("need --file and --func")
	}
	b := Resolve(root)
	store, _ := LoadAnnotations(b.Workspace)
	abs := filepath.Join(root, filepath.FromSlash(file))
	data, err := os.ReadFile(abs)
	if err != nil {
		return FocusHit{}, err
	}
	want := strings.ToLower(name)
	for _, fn := range extractFuncHits(string(data), file) {
		ln := strings.ToLower(fn.Name)
		if ln != want && !strings.HasSuffix(ln, "."+want) {
			continue
		}
		sum := fn.Summary
		learned := false
		for _, a := range store.Items {
			af, an := strings.ToLower(a.File), strings.ToLower(a.Func)
			if af != strings.ToLower(file) {
				continue
			}
			if an == want || an == ln || strings.HasSuffix(ln, "."+an) {
				sum = "[learned] " + strings.TrimPrefix(a.Summary, "[learned] ")
				learned = true
				break
			}
		}
		return FocusHit{
			File: file, Func: fn.Name, Kind: fn.Kind, Line: fn.Line,
			Summary: sum, Learned: learned, Reason: "lookup", NeedsLearn: !learned,
		}, nil
	}
	return FocusHit{}, fmt.Errorf("function %s not found in %s", name, file)
}

func gitChangedSourceFiles(root, since string) ([]string, error) {
	seen := map[string]bool{}
	addLines := func(out []byte) {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !looksLikeSource(line) {
				continue
			}
			seen[filepath.ToSlash(line)] = true
		}
	}
	// Unstaged + staged vs since
	cmds := [][]string{
		{"diff", "--name-only", since},
		{"diff", "--name-only", "--cached", since},
		{"ls-files", "--others", "--exclude-standard"},
	}
	for _, args := range cmds {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			continue
		}
		addLines(out)
	}
	var files []string
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

type lineRange struct{ start, end int }

func gitDiffHunks(root, since string) (map[string][]lineRange, error) {
	out, err := exec.Command("git", "-C", root, "diff", "-U0", since).Output()
	if err != nil {
		// try without since for working tree only
		out, err = exec.Command("git", "-C", root, "diff", "-U0").Output()
		if err != nil {
			return nil, err
		}
	}
	return parseDiffHunks(out), nil
}

func parseDiffHunks(diff []byte) map[string][]lineRange {
	res := map[string][]lineRange{}
	var cur string
	for _, raw := range bytes.Split(diff, []byte("\n")) {
		line := string(raw)
		if strings.HasPrefix(line, "+++ b/") {
			cur = filepath.ToSlash(strings.TrimPrefix(line, "+++ b/"))
			continue
		}
		if !strings.HasPrefix(line, "@@") || cur == "" {
			continue
		}
		// @@ -a,b +c,d @@
		plus := strings.Index(line, "+")
		if plus < 0 {
			continue
		}
		rest := line[plus+1:]
		sp := strings.IndexAny(rest, " ,")
		if sp < 0 {
			continue
		}
		start, _ := strconv.Atoi(rest[:sp])
		end := start
		if rest[sp] == ',' {
			rest2 := rest[sp+1:]
			sp2 := strings.IndexByte(rest2, ' ')
			if sp2 < 0 {
				sp2 = len(rest2)
			}
			n, _ := strconv.Atoi(rest2[:sp2])
			if n > 0 {
				end = start + n - 1
			}
		}
		if start <= 0 {
			continue
		}
		res[cur] = append(res[cur], lineRange{start: start, end: end})
	}
	return res
}

func lineInRanges(line int, ranges []lineRange) bool {
	if line <= 0 || len(ranges) == 0 {
		return false
	}
	for _, r := range ranges {
		// function header within ~2 lines of hunk or body overlap
		if line >= r.start-2 && line <= r.end+2 {
			return true
		}
	}
	return false
}

func looksLikeSource(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rb", ".rs", ".java", ".kt", ".swift", ".m", ".mm", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".php", ".vue", ".svelte":
		return true
	default:
		return false
	}
}
