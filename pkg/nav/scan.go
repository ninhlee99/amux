package nav

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirNames = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, ".next": true, ".nuxt": true, ".turbo": true,
	"target": true, "__pycache__": true, ".venv": true, "venv": true,
	".idea": true, ".vscode": true, ".cursor": true, ".claude": true,
	"tmp": true, "temp": true, "logs": true, "bin": true,
	"Pods": true, "DerivedData": true,
}

// ScanResult is a local filesystem snapshot used to generate maps (0 LLM tokens).
type ScanResult struct {
	Root       string
	Label      string
	Languages  []string
	Frameworks []string
	EntryFiles []string
	Modules    []ModuleHit
	TestHints  []string
	Readme     string
}

// ModuleHit is one navigable package/dir.
type ModuleHit struct {
	ID        string
	RelPath   string
	Keywords  []string
	ReadFirst []string
	Symbols   []string
	Tests     []string
	FileCount int
	FuncCount int
	Files     []FileHit
}

// ScanProject walks root (bounded) and classifies stack + modules.
func ScanProject(root string) (ScanResult, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return ScanResult{}, err
	}
	s := ScanResult{
		Root:  root,
		Label: filepath.Base(root),
	}
	detectStack(&s)
	s.EntryFiles = findEntries(root)
	if fileExists(filepath.Join(root, "README.md")) {
		s.Readme = "README.md"
	} else if fileExists(filepath.Join(root, "readme.md")) {
		s.Readme = "readme.md"
	}
	s.Modules = discoverModules(root, s.Languages)
	s.TestHints = discoverTestRoots(root)
	return s, nil
}

func detectStack(s *ScanResult) {
	root := s.Root
	add := func(lang string, fws ...string) {
		s.Languages = appendUnique(s.Languages, lang)
		for _, f := range fws {
			s.Frameworks = appendUnique(s.Frameworks, f)
		}
	}
	if fileExists(filepath.Join(root, "go.mod")) {
		add("go")
	}
	if fileExists(filepath.Join(root, "package.json")) {
		add("javascript")
		data, _ := os.ReadFile(filepath.Join(root, "package.json"))
		low := strings.ToLower(string(data))
		if strings.Contains(low, `"next"`) {
			add("javascript", "nextjs")
		}
		if strings.Contains(low, `"react"`) {
			add("javascript", "react")
		}
		if strings.Contains(low, `"vue"`) {
			add("javascript", "vue")
		}
		if strings.Contains(low, `"express"`) {
			add("javascript", "express")
		}
	}
	if fileExists(filepath.Join(root, "Gemfile")) {
		add("ruby")
		data, _ := os.ReadFile(filepath.Join(root, "Gemfile"))
		low := strings.ToLower(string(data))
		if strings.Contains(low, "rails") {
			add("ruby", "rails")
		}
	}
	if fileExists(filepath.Join(root, "Cargo.toml")) {
		add("rust")
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "requirements.txt")) || fileExists(filepath.Join(root, "setup.py")) {
		add("python")
		if fileExists(filepath.Join(root, "manage.py")) {
			add("python", "django")
		}
	}
	if fileExists(filepath.Join(root, "composer.json")) {
		add("php")
	}
	if fileExists(filepath.Join(root, "pom.xml")) || fileExists(filepath.Join(root, "build.gradle")) || fileExists(filepath.Join(root, "build.gradle.kts")) {
		add("java")
	}
	if len(s.Languages) == 0 {
		s.Languages = []string{"unknown"}
	}
}

func findEntries(root string) []string {
	candidates := []string{
		"main.go", "cmd", "package.json", "manage.py", "app.py", "main.py",
		"src/main.rs", "src/lib.rs", "index.js", "index.ts", "src/index.ts",
		"src/main.ts", "src/app.ts", "app/main.rb", "config/application.rb",
		"Cargo.toml", "go.mod", "Gemfile",
	}
	var out []string
	for _, c := range candidates {
		p := filepath.Join(root, c)
		if st, err := os.Stat(p); err == nil {
			if st.IsDir() {
				// prefer a file inside cmd/
				if c == "cmd" {
					_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
						if err != nil || d.IsDir() {
							return nil
						}
						if strings.HasSuffix(d.Name(), ".go") && len(out) < 5 {
							rel, _ := filepath.Rel(root, path)
							out = appendUnique(out, filepath.ToSlash(rel))
						}
						if len(out) >= 3 {
							return filepath.SkipAll
						}
						return nil
					})
					continue
				}
			}
			out = appendUnique(out, filepath.ToSlash(c))
		}
	}
	return out
}

func discoverModules(root string, langs []string) []ModuleHit {
	// Prefer known layer roots.
	layerRoots := []string{
		"pkg", "internal", "cmd", "src", "app", "lib", "services", "server",
		"api", "apps", "packages", "modules", "domain", "infra", "web",
		"controllers", "models", "views", "components", "hooks",
	}
	var hits []ModuleHit
	seen := map[string]bool{}

	for _, layer := range layerRoots {
		base := filepath.Join(root, layer)
		st, err := os.Stat(base)
		if err != nil || !st.IsDir() {
			continue
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		// If layer itself is a leaf package (few files), add layer.
		if isLeafPackage(base, langs) {
			if m := moduleFromDir(root, base); m.RelPath != "" && !seen[m.RelPath] {
				seen[m.RelPath] = true
				hits = append(hits, m)
			}
		}
		for _, e := range entries {
			if !e.IsDir() || skipDirNames[e.Name()] || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			sub := filepath.Join(base, e.Name())
			if m := moduleFromDir(root, sub); m.RelPath != "" && !seen[m.RelPath] {
				seen[m.RelPath] = true
				hits = append(hits, m)
			}
			if len(hits) >= 40 {
				break
			}
		}
		if len(hits) >= 40 {
			break
		}
	}

	// Top-level dirs that look like code
	if len(hits) < 5 {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if !e.IsDir() || skipDirNames[e.Name()] || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			sub := filepath.Join(root, e.Name())
			if !looksLikeCodeDir(sub, langs) {
				continue
			}
			if m := moduleFromDir(root, sub); m.RelPath != "" && !seen[m.RelPath] {
				seen[m.RelPath] = true
				hits = append(hits, m)
			}
			if len(hits) >= 40 {
				break
			}
		}
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].RelPath < hits[j].RelPath })
	return hits
}

func isLeafPackage(dir string, langs []string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if matchLangFile(e.Name(), langs) {
			files++
		}
	}
	return files > 0 && files <= 30
}

func looksLikeCodeDir(dir string, langs []string) bool {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirNames[d.Name()] {
				return filepath.SkipDir
			}
			if path != dir && strings.Count(path[len(dir):], string(os.PathSeparator)) > 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if matchLangFile(d.Name(), langs) {
			count++
			if count >= 2 {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return count >= 2
}

func matchLangFile(name string, langs []string) bool {
	low := strings.ToLower(name)
	extOK := strings.HasSuffix(low, ".go") || strings.HasSuffix(low, ".ts") || strings.HasSuffix(low, ".tsx") ||
		strings.HasSuffix(low, ".js") || strings.HasSuffix(low, ".jsx") || strings.HasSuffix(low, ".rb") ||
		strings.HasSuffix(low, ".py") || strings.HasSuffix(low, ".rs") || strings.HasSuffix(low, ".java") ||
		strings.HasSuffix(low, ".kt") || strings.HasSuffix(low, ".php") || strings.HasSuffix(low, ".swift")
	if !extOK {
		return false
	}
	_ = langs
	return true
}

func moduleFromDir(root, dir string) ModuleHit {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return ModuleHit{}
	}
	rel = filepath.ToSlash(rel)
	base := filepath.Base(dir)
	id := sanitizeSlug(strings.ReplaceAll(rel, "/", "-"))
	if id == "" {
		id = sanitizeSlug(base)
	}

	readFirst := pickReadFirst(dir, root)
	tests := pickTests(dir, root)
	keywords := keywordsFor(rel, base)
	symbols := guessSymbols(readFirst, root)

	m := ModuleHit{
		ID:        id,
		RelPath:   rel,
		Keywords:  keywords,
		ReadFirst: readFirst,
		Symbols:   symbols,
		Tests:     tests,
	}
	enrichModuleInventory(root, &m)
	return m
}

func pickReadFirst(dir, root string) []string {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefer := []string{"mod.go", "main.go", "index.ts", "index.tsx", "index.js", "lib.rs", "main.rs",
		"application.rb", "routes.rb", "__init__.py", "mod.rs"}
	for _, p := range prefer {
		for _, e := range entries {
			if !e.IsDir() && e.Name() == p {
				rel, _ := filepath.Rel(root, filepath.Join(dir, e.Name()))
				files = append(files, filepath.ToSlash(rel))
			}
		}
	}
	if len(files) == 0 {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasSuffix(n, "_test.go") || strings.Contains(n, ".test.") || strings.Contains(n, ".spec.") {
				continue
			}
			if matchLangFile(n, nil) {
				rel, _ := filepath.Rel(root, filepath.Join(dir, n))
				files = append(files, filepath.ToSlash(rel))
				if len(files) >= 3 {
					break
				}
			}
		}
	}
	if len(files) > 3 {
		files = files[:3]
	}
	return files
}

func pickTests(dir, root string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && path != dir && skipDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, "_test.go") || strings.Contains(n, ".test.") || strings.Contains(n, ".spec.") ||
			strings.HasSuffix(n, "_spec.rb") || strings.HasPrefix(n, "test_") {
			rel, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(rel))
			if len(out) >= 3 {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return out
}

func discoverTestRoots(root string) []string {
	var out []string
	for _, c := range []string{"test", "tests", "spec", "__tests__", "pkg"} {
		p := filepath.Join(root, c)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, filepath.ToSlash(c))
		}
	}
	return out
}

func keywordsFor(rel, base string) []string {
	parts := strings.Split(rel, "/")
	kw := []string{base}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." {
			continue
		}
		kw = appendUnique(kw, strings.ToLower(p))
	}
	// Synonyms for common layer names
	switch strings.ToLower(base) {
	case "auth", "authentication", "session", "sessions":
		kw = appendUnique(kw, "login", "auth", "session", "jwt")
	case "api", "handlers", "controller", "controllers", "routes":
		kw = appendUnique(kw, "api", "http", "endpoint", "route")
	case "db", "database", "models", "repo", "repository", "repositories":
		kw = appendUnique(kw, "database", "model", "sql", "persist")
	case "ui", "views", "components", "pages":
		kw = appendUnique(kw, "ui", "frontend", "component")
	case "cli", "cmd":
		kw = appendUnique(kw, "cli", "command")
	case "proxy", "gateway":
		kw = appendUnique(kw, "proxy", "gateway", "http")
	}
	if len(kw) > 8 {
		kw = kw[:8]
	}
	return kw
}

func guessSymbols(readFirst []string, root string) []string {
	var syms []string
	for _, rel := range readFirst {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		syms = append(syms, extractSymbols(string(data), rel)...)
		if len(syms) >= 6 {
			break
		}
	}
	if len(syms) > 6 {
		syms = syms[:6]
	}
	return syms
}

func extractSymbols(src, rel string) []string {
	var out []string
	lines := strings.Split(src, "\n")
	ext := strings.ToLower(filepath.Ext(rel))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch ext {
		case ".go":
			if strings.HasPrefix(line, "func ") {
				name := goFuncName(line)
				if name != "" && !strings.HasPrefix(name, "Test") {
					out = appendUnique(out, name)
				}
			}
			if strings.HasPrefix(line, "type ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					out = appendUnique(out, fields[1])
				}
			}
		case ".ts", ".tsx", ".js", ".jsx":
			if strings.HasPrefix(line, "export function ") || strings.HasPrefix(line, "function ") ||
				strings.HasPrefix(line, "export const ") || strings.HasPrefix(line, "export class ") {
				fields := strings.Fields(line)
				for i, f := range fields {
					if f == "function" || f == "const" || f == "class" {
						if i+1 < len(fields) {
							name := strings.Trim(fields[i+1], "({:=;")
							if name != "" {
								out = appendUnique(out, name)
							}
						}
						break
					}
				}
			}
		case ".rb":
			if strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "def ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					out = appendUnique(out, strings.Trim(fields[1], "()"))
				}
			}
		case ".py":
			if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					out = appendUnique(out, strings.Trim(fields[1], "():"))
				}
			}
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func goFuncName(line string) string {
	// func (r *T) Name( or func Name(
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		if i := strings.Index(line, ")"); i >= 0 && i+1 < len(line) {
			line = strings.TrimSpace(line[i+1:])
		}
	}
	name := line
	if i := strings.IndexAny(line, " ("); i >= 0 {
		name = line[:i]
	}
	return name
}

func appendUnique(list []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		found := false
		for _, x := range list {
			if strings.EqualFold(x, v) {
				found = true
				break
			}
		}
		if !found {
			list = append(list, v)
		}
	}
	return list
}
