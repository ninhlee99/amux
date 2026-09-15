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
	graphPath := filepath.Join(ws, FileGraphMD)

	write := func(path, body string, always bool) error {
		if !always && !force && fileExists(path) {
			return nil
		}
		return os.WriteFile(path, []byte(body), 0o644)
	}

	graph := buildGraph(root, scan)

	if err := write(mapPath, renderMapMD(scan, b.Workspace), true); err != nil {
		return b, err
	}
	if err := write(locatePath, renderLocateYAML(scan, graph), true); err != nil {
		return b, err
	}
	if err := write(graphPath, renderGraphMD(scan, graph, b.Workspace), true); err != nil {
		return b, err
	}
	if htmlBytes, err := RenderGraphHTML(scan, graph, ""); err == nil {
		_ = write(filepath.Join(ws, FileGraphHTML), string(htmlBytes), true)
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
	if err := publishAmuxInventory(b, scan, learned, totalFiles, totalFuncs); err != nil {
		return b, err
	}
	return b, nil
}

// publishAmuxInventory writes portable inventory into docs/ for the amux tool repo
// (does not overwrite hand-curated AI_CODEBASE_MAP.md or ai-locate.yaml).
// root in MAP_GENERATED is always "." — no machine-specific absolute path.
func publishAmuxInventory(b Bundle, scan ScanResult, learned, totalFiles, totalFuncs int) error {
	if !b.IsAmuxRepository() {
		return nil
	}
	docs := filepath.Join(b.ProjectRoot, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		return err
	}
	srcGraph := filepath.Join(b.WorkspaceDir, FileGraphMD)
	if fileExists(srcGraph) {
		body, err := os.ReadFile(srcGraph)
		if err != nil {
			return err
		}
		portable := strings.Replace(string(body),
			fmt.Sprintf("> Workspace: ~/.am/workspaces/%s/", b.Workspace),
			"> docs/GRAPH.md (portable · root=.)",
			1)
		if err := os.WriteFile(filepath.Join(docs, FileGraphMD), []byte(portable), 0o644); err != nil {
			return err
		}
	}
	if fileExists(filepath.Join(b.WorkspaceDir, FileGraphHTML)) {
		_ = copyFile(filepath.Join(b.WorkspaceDir, FileGraphHTML), filepath.Join(docs, FileGraphHTML))
	}
	// MODULES.md = stub only (AI uses GRAPH); still publish stub for clarity
	srcMod := filepath.Join(b.WorkspaceDir, FileModulesMD)
	if fileExists(srcMod) {
		body, err := os.ReadFile(srcMod)
		if err != nil {
			return err
		}
		portable := strings.Replace(string(body),
			fmt.Sprintf("> ~/.am/workspaces/%s/MODULES.md", b.Workspace),
			"> docs/MODULES.md (stub · see GRAPH.md)",
			1)
		if err := os.WriteFile(filepath.Join(docs, FileModulesMD), []byte(portable), 0o644); err != nil {
			return err
		}
	}
	meta := fmt.Sprintf("generated_at=%s\nroot=.\nmodules=%d\nfiles=%d\nfuncs=%d\nlearned=%d\nmode=local-scan\ntokens=0\nnote=docs only: root=. is portable; real abs path lives in ~/.am/workspaces/*/project_root.txt\n",
		time.Now().Format(time.RFC3339), len(scan.Modules), totalFiles, totalFuncs, learned)
	return os.WriteFile(filepath.Join(docs, "MAP_GENERATED.txt"), []byte(meta), 0o644)
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

1. GRAPH.md — neural mesh (module↔module) + func subnets (đọc cái này)
2. AI_CODEBASE_MAP.md — tổng quan bảng module
3. ai-locate.yaml — keywords → hubs (không dump mọi func)
4. annotations.json — learn overlays

Cập nhật: am map update
Chi tiết 1 func: am map get / am map learn
Cấm dump MODULES cũ kiểu bảng dài.
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
	b.WriteString("2. Xem **GRAPH.md** (mesh module + subnet func) — không đọc bảng inventory dài\n")
	b.WriteString("3. Mở đúng file hub — `am map get` nếu cần 1 summary\n")
	b.WriteString("4. `am map update` khi đổi cấu trúc lớn\n")
	b.WriteString("5. Đọc sâu xong → `am map learn`\n\n")

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
	b.WriteString("\nBản đồ nơ-ron: **[GRAPH.md](./GRAPH.md)** · 1 func: `am map get`\n")
	return b.String()
}

func renderModulesMD(scan ScanResult, workspace string) string {
	// Stub only — full dump burned tokens; GRAPH.md is the AI surface.
	var b strings.Builder
	fmt.Fprintf(&b, "# MODULES — %s (stub)\n\n", scan.Label)
	fmt.Fprintf(&b, "> ~/.am/workspaces/%s/MODULES.md  \n", workspace)
	b.WriteString("> **Deprecated as AI dump.** Dùng [GRAPH.md](./GRAPH.md) (mesh + subnets).\n")
	b.WriteString("> Chi tiết 1 function: `am map get --file PATH --func NAME`\n\n")
	b.WriteString("| module | files | funcs |\n|--------|------:|------:|\n")
	for _, m := range scan.Modules {
		fmt.Fprintf(&b, "| `%s` | %d | %d |\n", m.RelPath, m.FileCount, m.FuncCount)
	}
	return b.String()
}

func renderLocateYAML(scan ScanResult, graph GraphSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — auto-generated by amux map (local scan)\n", scan.Label)
	b.WriteString("version: 3\n")
	b.WriteString("scope: client-project\n")
	fmt.Fprintf(&b, "project: %q\n", scan.Label)
	b.WriteString("root: \".\"\n")
	fmt.Fprintf(&b, "generated: %q\n", time.Now().Format(time.RFC3339))
	b.WriteString("generator: amux-local-scan\n")
	b.WriteString("detail: GRAPH.md\n")
	b.WriteString("token_rule: \"read GRAPH mesh+one subnet; never dump all funcs\"\n")
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
	b.WriteString("    keywords: [overview, architecture, entry, bootstrap, readme, graph]\n")
	b.WriteString("    read_first:\n")
	b.WriteString("      - GRAPH.md\n")
	if scan.Readme != "" {
		fmt.Fprintf(&b, "      - %s\n", scan.Readme)
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
		hubs := graph.Hubs[m.RelPath]
		b.WriteString("    symbols: [")
		syms := hubs
		if len(syms) == 0 {
			syms = m.Symbols
			if len(syms) > maxHubsPerMod {
				syms = syms[:maxHubsPerMod]
			}
		}
		for i, s := range syms {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", yamlBareOrQuote(shortFuncName(s)))
		}
		b.WriteString("]\n")
		if len(m.Tests) == 0 {
			b.WriteString("    tests: []\n")
		} else {
			b.WriteString("    tests:\n")
			n := 0
			for _, t := range m.Tests {
				fmt.Fprintf(&b, "      - %s\n", t)
				n++
				if n >= 3 {
					break
				}
			}
		}
	}

	b.WriteString("\ninvariants:\n")
	b.WriteString("  - \"Đọc GRAPH.md (mesh + 1 subnet) — không dump mọi function\"\n")
	b.WriteString("  - \"Chi tiết 1 func: am map get / learn\"\n")
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
