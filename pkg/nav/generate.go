package nav

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InitScaffold scans the project and writes a real map into
// ~/.am/workspaces/<name>/. force=true regenerates map+locate.
func InitScaffold(startDir string, force bool) (Bundle, error) {
	return GenerateMap(startDir, force)
}

// GenerateMap always (re)builds navigation files from a local filesystem scan.
// Does not call any LLM — 0 API tokens.
func GenerateMap(startDir string, force bool) (Bundle, error) {
	root := GitRoot(startDir)
	if root == "" {
		return Bundle{}, fmt.Errorf("cannot resolve project directory")
	}
	b := Resolve(root)
	ws := WorkspaceDir(b.Workspace)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return b, err
	}

	scan, err := ScanProject(root)
	if err != nil {
		return b, err
	}
	store, _ := LoadAnnotations(b.Workspace)
	learned := ApplyAnnotations(&scan, store)

	mapPath := filepath.Join(ws, FileMapMD)
	locatePath := filepath.Join(ws, FileLocateYAML)
	agentsPath := filepath.Join(ws, FileAgentsMD)
	modulesPath := filepath.Join(ws, FileModulesMD)

	write := func(path, body string, always bool) error {
		if !always && !force && fileExists(path) {
			return nil
		}
		return os.WriteFile(path, []byte(body), 0o644)
	}

	if err := write(mapPath, renderMapMD(scan, b.Workspace), true); err != nil {
		return b, err
	}
	if err := write(locatePath, renderLocateYAML(scan), true); err != nil {
		return b, err
	}
	if err := write(modulesPath, renderModulesMD(scan, b.Workspace), true); err != nil {
		return b, err
	}
	if err := write(agentsPath, renderAgentsMD(scan, b.Workspace), force || !fileExists(agentsPath)); err != nil {
		return b, err
	}
	if err := os.WriteFile(filepath.Join(ws, FileRootMeta), []byte(b.ProjectRoot+"\n"), 0o644); err != nil {
		return b, err
	}
	totalFiles, totalFuncs := 0, 0
	for _, m := range scan.Modules {
		totalFiles += m.FileCount
		totalFuncs += m.FuncCount
	}
	_ = os.WriteFile(filepath.Join(ws, "GENERATED.txt"),
		[]byte(fmt.Sprintf("generated_at=%s\nroot=%s\nmodules=%d\nfiles=%d\nfuncs=%d\nlearned=%d\nmode=local-scan\ntokens=0\n",
			time.Now().Format(time.RFC3339), scan.Root, len(scan.Modules), totalFiles, totalFuncs, learned)), 0o644)

	b = Resolve(root)
	if err := publishAmuxInventory(b); err != nil {
		return b, err
	}
	return b, nil
}

// publishAmuxInventory copies scan inventory into docs/ for the amux tool repo
// (does not overwrite hand-curated AI_CODEBASE_MAP.md or ai-locate.yaml).
func publishAmuxInventory(b Bundle) error {
	if !b.IsAmuxRepository() {
		return nil
	}
	docs := filepath.Join(b.ProjectRoot, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		return err
	}
	srcMod := filepath.Join(b.WorkspaceDir, FileModulesMD)
	if fileExists(srcMod) {
		if err := copyFile(srcMod, filepath.Join(docs, FileModulesMD)); err != nil {
			return err
		}
	}
	srcGen := filepath.Join(b.WorkspaceDir, "GENERATED.txt")
	if fileExists(srcGen) {
		if err := copyFile(srcGen, filepath.Join(docs, "MAP_GENERATED.txt")); err != nil {
			return err
		}
	}
	return nil
}

// UpdateMap regenerates map from current tree (same as init --force).
func UpdateMap(startDir string) (Bundle, error) {
	return GenerateMap(startDir, true)
}

// EnsureMapIfMissing creates a map on first touch when none exists.
func EnsureMapIfMissing(projectRoot string) (Bundle, error) {
	b := Resolve(projectRoot)
	if b.ProjectRoot == "" {
		return b, fmt.Errorf("empty project root")
	}
	if b.HasProjectMap() && fileExists(b.WorkspaceMap) {
		_ = EnsureWorkspace(b)
		return b, nil
	}
	return GenerateMap(projectRoot, false)
}

func renderAgentsMD(scan ScanResult, workspace string) string {
	return fmt.Sprintf(`# Agent — %s

Bản đồ **tự sinh** bởi amux (local scan, 0 token API).

Workspace: ~/.am/workspaces/%s/

1. AI_CODEBASE_MAP.md — tổng quan + bảng module (số file/func)
2. MODULES.md — chi tiết từng file + function + mô tả
3. ai-locate.yaml — index keywords
4. annotations.json — mô tả do AI/học lại sau khi đọc sâu (sống qua map update)

Cập nhật cấu trúc: am map update
Sau khi phân tích function: am map learn --file PATH --func NAME --summary "..."

Đây là project client (qua proxy), không phải repo amux.
`, scan.Label, workspace)
}

func renderMapMD(scan ScanResult, workspace string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Bản đồ codebase — %s\n\n", scan.Label)
	fmt.Fprintf(&b, "> Project root: %s  \n", scan.Root)
	fmt.Fprintf(&b, "> Workspace: ~/.am/workspaces/%s/  \n", workspace)
	fmt.Fprintf(&b, "> Generated: amux local scan (0 LLM tokens) · %s\n\n", time.Now().Format("2006-01-02 15:04"))

	b.WriteString("## Quy trình agent\n\n")
	b.WriteString("1. Match mục tiêu → `ai-locate.yaml` (`keywords`)\n")
	b.WriteString("2. Xem bảng module (số file / số func) bên dưới\n")
	b.WriteString("3. Đọc chi tiết function trong `MODULES.md`\n")
	b.WriteString("4. Mở đúng file:line — không quét cả repo\n")
	b.WriteString("5. `am map update` khi đổi cấu trúc\n")
	b.WriteString("6. Sau khi đọc sâu 1 function → `am map learn` để lần sau summary đúng hơn\n\n")

	b.WriteString("## Stack\n\n")
	fmt.Fprintf(&b, "- Languages: %s\n", strings.Join(scan.Languages, ", "))
	if len(scan.Frameworks) > 0 {
		fmt.Fprintf(&b, "- Frameworks: %s\n", strings.Join(scan.Frameworks, ", "))
	}
	if len(scan.EntryFiles) > 0 {
		fmt.Fprintf(&b, "- Entry: %s\n", strings.Join(scan.EntryFiles, ", "))
	}
	if scan.Readme != "" {
		fmt.Fprintf(&b, "- README: %s\n", scan.Readme)
	}

	totalFiles, totalFuncs := 0, 0
	for _, m := range scan.Modules {
		totalFiles += m.FileCount
		totalFuncs += m.FuncCount
	}
	fmt.Fprintf(&b, "\n## Modules (%d) · files %d · funcs %d\n\n", len(scan.Modules), totalFiles, totalFuncs)
	b.WriteString("| Module | files | funcs | keywords | entry |\n")
	b.WriteString("|--------|------:|------:|----------|-------|\n")
	for _, m := range scan.Modules {
		rf := "-"
		if len(m.ReadFirst) > 0 {
			rf = m.ReadFirst[0]
		}
		fmt.Fprintf(&b, "| `%s` | %d | %d | %s | %s |\n",
			m.RelPath, m.FileCount, m.FuncCount, strings.Join(m.Keywords, ", "), rf)
	}
	if len(scan.Modules) == 0 {
		b.WriteString("| *(scan không thấy module)* | | | | |\n")
	}
	b.WriteString("\nChi tiết từng file/function: **[MODULES.md](./MODULES.md)**\n")
	return b.String()
}

func renderModulesMD(scan ScanResult, workspace string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Module inventory — %s\n\n", scan.Label)
	fmt.Fprintf(&b, "> ~/.am/workspaces/%s/MODULES.md  \n", workspace)
	fmt.Fprintf(&b, "> Auto-generated · doc-comment + name heuristic (không LLM)\n\n")
	b.WriteString("Mỗi module: số file, mỗi file: số function, mỗi function: signature + mô tả ngắn.\n\n")

	for _, m := range scan.Modules {
		fmt.Fprintf(&b, "## `%s` — %d files · %d funcs\n\n", m.RelPath, m.FileCount, m.FuncCount)
		if len(m.Keywords) > 0 {
			fmt.Fprintf(&b, "Keywords: %s\n\n", strings.Join(m.Keywords, ", "))
		}
		if len(m.Files) == 0 {
			b.WriteString("*(không có source file được parse)*\n\n")
			continue
		}
		for _, f := range m.Files {
			fmt.Fprintf(&b, "### `%s` (%d funcs)\n\n", f.RelPath, f.FuncCount)
			if f.FuncCount == 0 {
				b.WriteString("- *(no exported/top-level funcs detected)*\n\n")
				continue
			}
			b.WriteString("| Line | Kind | Name | Summary |\n")
			b.WriteString("|-----:|------|------|---------|\n")
			for _, fn := range f.Funcs {
				sum := strings.ReplaceAll(fn.Summary, "|", "/")
				fmt.Fprintf(&b, "| %d | %s | `%s` | %s |\n", fn.Line, fn.Kind, fn.Name, sum)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderLocateYAML(scan ScanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — auto-generated by amux map (local scan)\n", scan.Label)
	b.WriteString("version: 2\n")
	b.WriteString("scope: client-project\n")
	fmt.Fprintf(&b, "project: %q\n", scan.Label)
	fmt.Fprintf(&b, "root: %q\n", scan.Root)
	fmt.Fprintf(&b, "generated: %q\n", time.Now().Format(time.RFC3339))
	b.WriteString("generator: amux-local-scan\n")
	b.WriteString("detail: MODULES.md\n")
	b.WriteString("\nlanguages:\n")
	for _, l := range scan.Languages {
		fmt.Fprintf(&b, "  - %s\n", l)
	}
	if len(scan.Frameworks) > 0 {
		b.WriteString("frameworks:\n")
		for _, f := range scan.Frameworks {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	b.WriteString("\ntasks:\n")

	b.WriteString("  - id: project-overview\n")
	b.WriteString("    keywords: [overview, architecture, entry, bootstrap, readme]\n")
	b.WriteString("    read_first:\n")
	if scan.Readme != "" {
		fmt.Fprintf(&b, "      - %s\n", scan.Readme)
	}
	for i, e := range scan.EntryFiles {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&b, "      - %s\n", e)
	}
	if scan.Readme == "" && len(scan.EntryFiles) == 0 {
		b.WriteString("      - .\n")
	}
	b.WriteString("    symbols: []\n")
	b.WriteString("    tests: []\n")

	for _, m := range scan.Modules {
		fmt.Fprintf(&b, "  - id: %s\n", m.ID)
		fmt.Fprintf(&b, "    path: %s\n", m.RelPath)
		fmt.Fprintf(&b, "    file_count: %d\n", m.FileCount)
		fmt.Fprintf(&b, "    func_count: %d\n", m.FuncCount)
		b.WriteString("    keywords: [")
		for i, k := range m.Keywords {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", yamlBareOrQuote(k))
		}
		b.WriteString("]\n")
		b.WriteString("    read_first:\n")
		if len(m.ReadFirst) == 0 {
			fmt.Fprintf(&b, "      - %s\n", m.RelPath)
		} else {
			for _, f := range m.ReadFirst {
				fmt.Fprintf(&b, "      - %s\n", f)
			}
		}
		b.WriteString("    symbols: [")
		for i, s := range m.Symbols {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", yamlBareOrQuote(s))
		}
		b.WriteString("]\n")
		if len(m.Tests) == 0 {
			b.WriteString("    tests: []\n")
		} else {
			b.WriteString("    tests:\n")
			for _, t := range m.Tests {
				fmt.Fprintf(&b, "      - %s\n", t)
			}
		}
		b.WriteString("    files:\n")
		if len(m.Files) == 0 {
			b.WriteString("      []\n")
			continue
		}
		for _, f := range m.Files {
			fmt.Fprintf(&b, "      - path: %s\n", f.RelPath)
			fmt.Fprintf(&b, "        func_count: %d\n", f.FuncCount)
			if len(f.Funcs) == 0 {
				b.WriteString("        functions: []\n")
				continue
			}
			b.WriteString("        functions:\n")
			for _, fn := range f.Funcs {
				fmt.Fprintf(&b, "          - name: %s\n", yamlBareOrQuote(fn.Name))
				fmt.Fprintf(&b, "            kind: %s\n", fn.Kind)
				fmt.Fprintf(&b, "            line: %d\n", fn.Line)
				fmt.Fprintf(&b, "            summary: %q\n", fn.Summary)
			}
		}
	}

	b.WriteString("\ninvariants:\n")
	b.WriteString("  - \"Dùng bản đồ workspace này — không dùng map repo amux\"\n")
	b.WriteString("  - \"Chi tiết function nằm trong MODULES.md + files[].functions\"\n")
	b.WriteString("  - \"am map update sau khi đổi cấu trúc lớn\"\n")
	return b.String()
}

func yamlBareOrQuote(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '*' || r == '(' || r == ')' {
			continue
		}
		return fmt.Sprintf("%q", s)
	}
	return s
}
