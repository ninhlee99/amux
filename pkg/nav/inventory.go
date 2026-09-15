package nav

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	maxFilesPerModule = 60
	maxFuncsPerFile   = 80
)

// FuncHit is one function/method/class extracted from source.
type FuncHit struct {
	Name    string // Login, (*Session).Save
	Kind    string // func | method | type | class | def
	Line    int
	Sig     string // short signature line
	Summary string // from doc comment or name heuristic
}

// FileHit is one source file inside a module.
type FileHit struct {
	RelPath   string
	FuncCount int
	Funcs     []FuncHit
}

// enrichModuleInventory fills Files / counts for a module directory.
func enrichModuleInventory(root string, m *ModuleHit) {
	dir := filepath.Join(root, filepath.FromSlash(m.RelPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []FileHit
	var allSyms []string
	for _, e := range entries {
		if e.IsDir() || skipDirNames[e.Name()] {
			continue
		}
		n := e.Name()
		if isTestFileName(n) {
			continue
		}
		if !matchLangFile(n, nil) {
			continue
		}
		abs := filepath.Join(dir, n)
		rel, _ := filepath.Rel(root, abs)
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		funcs := extractFuncHits(string(data), rel)
		if len(funcs) > maxFuncsPerFile {
			funcs = funcs[:maxFuncsPerFile]
		}
		fh := FileHit{RelPath: rel, FuncCount: len(funcs), Funcs: funcs}
		files = append(files, fh)
		for _, f := range funcs {
			allSyms = appendUnique(allSyms, f.Name)
		}
		if len(files) >= maxFilesPerModule {
			break
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	m.Files = files
	m.FileCount = len(files)
	m.FuncCount = 0
	for _, f := range files {
		m.FuncCount += f.FuncCount
	}
	if len(allSyms) > 0 {
		m.Symbols = allSyms
		if len(m.Symbols) > 24 {
			m.Symbols = m.Symbols[:24]
		}
	}
	// Prefer files with most funcs as read_first
	if len(files) > 0 {
		sorted := append([]FileHit(nil), files...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].FuncCount != sorted[j].FuncCount {
				return sorted[i].FuncCount > sorted[j].FuncCount
			}
			return sorted[i].RelPath < sorted[j].RelPath
		})
		m.ReadFirst = nil
		for i := 0; i < len(sorted) && i < 3; i++ {
			m.ReadFirst = append(m.ReadFirst, sorted[i].RelPath)
		}
	}
}

func isTestFileName(n string) bool {
	low := strings.ToLower(n)
	return strings.HasSuffix(low, "_test.go") ||
		strings.Contains(low, ".test.") ||
		strings.Contains(low, ".spec.") ||
		strings.HasSuffix(low, "_spec.rb") ||
		strings.HasPrefix(low, "test_")
}

// extractFuncHits parses functions with line, signature, and summary.
func extractFuncHits(src, rel string) []FuncHit {
	lines := strings.Split(src, "\n")
	ext := strings.ToLower(filepath.Ext(rel))
	var out []FuncHit
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var hit *FuncHit
		switch ext {
		case ".go":
			if strings.HasPrefix(line, "func ") {
				name := goFuncName(line)
				if name == "" || strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") {
					break
				}
				kind := "func"
				rest := strings.TrimPrefix(line, "func ")
				if strings.HasPrefix(strings.TrimSpace(rest), "(") {
					kind = "method"
				}
				hit = &FuncHit{Name: name, Kind: kind, Line: i + 1, Sig: truncate(line, 120)}
			} else if strings.HasPrefix(line, "type ") && strings.Contains(line, " struct") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					hit = &FuncHit{Name: fields[1], Kind: "type", Line: i + 1, Sig: truncate(line, 120)}
				}
			} else if strings.HasPrefix(line, "type ") && strings.Contains(line, " interface") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					hit = &FuncHit{Name: fields[1], Kind: "type", Line: i + 1, Sig: truncate(line, 120)}
				}
			}
		case ".ts", ".tsx", ".js", ".jsx":
			if strings.HasPrefix(line, "export function ") || strings.HasPrefix(line, "function ") ||
				strings.HasPrefix(line, "export async function ") || strings.HasPrefix(line, "async function ") ||
				strings.HasPrefix(line, "export class ") || strings.HasPrefix(line, "class ") {
				fields := strings.Fields(line)
				for j, f := range fields {
					if f == "function" || f == "class" {
						if j+1 < len(fields) {
							name := strings.Trim(fields[j+1], "({:=;<")
							if name != "" {
								kind := "func"
								if f == "class" {
									kind = "class"
								}
								hit = &FuncHit{Name: name, Kind: kind, Line: i + 1, Sig: truncate(line, 120)}
							}
						}
						break
					}
				}
			}
		case ".rb":
			if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "module ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kind := "def"
					if fields[0] == "class" || fields[0] == "module" {
						kind = fields[0]
					}
					hit = &FuncHit{Name: strings.Trim(fields[1], "()"), Kind: kind, Line: i + 1, Sig: truncate(line, 120)}
				}
			}
		case ".py":
			if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "async def ") || strings.HasPrefix(line, "class ") {
				fields := strings.Fields(line)
				idx := 1
				kind := "def"
				if fields[0] == "async" {
					idx = 2
				}
				if fields[0] == "class" {
					kind = "class"
				}
				if idx < len(fields) {
					hit = &FuncHit{Name: strings.Trim(fields[idx], "():"), Kind: kind, Line: i + 1, Sig: truncate(line, 120)}
				}
			}
		case ".rs":
			if strings.HasPrefix(line, "pub fn ") || strings.HasPrefix(line, "fn ") ||
				strings.HasPrefix(line, "pub struct ") || strings.HasPrefix(line, "struct ") {
				fields := strings.Fields(line)
				for j, f := range fields {
					if f == "fn" || f == "struct" {
						if j+1 < len(fields) {
							name := strings.Trim(fields[j+1], "<({")
							kind := "func"
							if f == "struct" {
								kind = "type"
							}
							hit = &FuncHit{Name: name, Kind: kind, Line: i + 1, Sig: truncate(line, 120)}
						}
						break
					}
				}
			}
		}
		if hit == nil {
			continue
		}
		hit.Summary = docCommentAbove(lines, i, ext)
		if hit.Summary == "" {
			hit.Summary = summarizeFromName(hit.Name, hit.Kind)
		}
		out = append(out, *hit)
		if len(out) >= maxFuncsPerFile {
			break
		}
	}
	return out
}

func docCommentAbove(lines []string, idx int, ext string) string {
	var parts []string
	for j := idx - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		switch ext {
		case ".go", ".ts", ".tsx", ".js", ".jsx", ".java", ".kt", ".rs":
			if strings.HasPrefix(t, "//") {
				parts = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "//"))}, parts...)
				continue
			}
			if strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") || strings.HasSuffix(t, "*/") {
				t = strings.TrimSpace(strings.TrimPrefix(t, "/*"))
				t = strings.TrimSpace(strings.TrimPrefix(t, "*"))
				t = strings.TrimSpace(strings.TrimSuffix(t, "*/"))
				if t != "" {
					parts = append([]string{t}, parts...)
				}
				continue
			}
		case ".py":
			if strings.HasPrefix(t, "#") {
				parts = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "#"))}, parts...)
				continue
			}
			if strings.HasPrefix(t, `"""`) || strings.HasPrefix(t, "'''") {
				continue
			}
		case ".rb":
			if strings.HasPrefix(t, "#") {
				parts = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "#"))}, parts...)
				continue
			}
		}
		break
	}
	s := strings.Join(parts, " ")
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, 160)
}

func summarizeFromName(name, kind string) string {
	name = strings.TrimPrefix(name, "*")
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if kind == "type" || kind == "class" {
		return "type/class " + name
	}
	// Split CamelCase / snake
	words := splitIdent(name)
	if len(words) == 0 {
		return name
	}
	verbs := map[string]string{
		"get": "get", "set": "set", "new": "create", "create": "create", "update": "update",
		"delete": "delete", "remove": "remove", "list": "list", "find": "find", "load": "load",
		"save": "save", "parse": "parse", "handle": "handle", "render": "render", "build": "build",
		"init": "initialize", "start": "start", "stop": "stop", "run": "run", "login": "login",
		"auth": "authenticate", "validate": "validate", "send": "send", "fetch": "fetch",
	}
	w0 := strings.ToLower(words[0])
	if v, ok := verbs[w0]; ok {
		rest := strings.Join(words[1:], " ")
		if rest != "" {
			return v + " " + strings.ToLower(rest)
		}
		return v
	}
	return strings.ToLower(strings.Join(words, " "))
}

func splitIdent(s string) []string {
	s = strings.ReplaceAll(s, "_", " ")
	var b strings.Builder
	var out []string
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		if r == ' ' {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			flush()
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
