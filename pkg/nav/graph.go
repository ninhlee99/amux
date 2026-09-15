package nav

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	FileGraphMD     = "GRAPH.md"
	maxMeshEdges    = 120
	maxSubnetEdges  = 36
	maxHubsPerMod   = 6
	maxImportScanKB = 512
)

// ModEdge is module→module link (import / dependency).
type ModEdge struct {
	From, To string
}

// FuncEdge is function→function link inside one module.
type FuncEdge struct {
	From, To string
}

// GraphSnapshot is a compact neural-style map for AI (low tokens).
type GraphSnapshot struct {
	Mesh     []ModEdge
	Subnets  map[string][]FuncEdge // module path → edges
	Hubs     map[string][]string   // module path → hub func names
	ModOrder []string
}

var (
	reGoImportLine   = regexp.MustCompile(`^\s*"([^"]+)"\s*$`)
	reGoImportSingle = regexp.MustCompile(`^\s*import\s+(?:\w+\s+)?"([^"]+)"`)
	// call-ish: Foo( or Foo.Bar — keep short exported-looking names
	reFuncCall = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]{2,})\s*\(`)
)

func buildGraph(root string, scan ScanResult) GraphSnapshot {
	g := GraphSnapshot{
		Subnets: map[string][]FuncEdge{},
		Hubs:    map[string][]string{},
	}
	modByPath := map[string]*ModuleHit{}
	for i := range scan.Modules {
		m := &scan.Modules[i]
		modByPath[m.RelPath] = m
		g.ModOrder = append(g.ModOrder, m.RelPath)
	}
	sort.Strings(g.ModOrder)

	modulePrefix := goModulePath(root) // e.g. amux-accounts
	pathByImport := map[string]string{}
	for _, m := range scan.Modules {
		// "amux-accounts/pkg/nav" → pkg/nav
		if modulePrefix != "" {
			pathByImport[modulePrefix+"/"+m.RelPath] = m.RelPath
		}
		pathByImport[m.RelPath] = m.RelPath
		pathByImport[filepath.Base(m.RelPath)] = m.RelPath
	}

	edgeSet := map[string]bool{}
	addMesh := func(from, to string) {
		if from == "" || to == "" || from == to {
			return
		}
		if _, ok := modByPath[from]; !ok {
			return
		}
		if _, ok := modByPath[to]; !ok {
			return
		}
		k := from + "->" + to
		if edgeSet[k] {
			return
		}
		edgeSet[k] = true
		g.Mesh = append(g.Mesh, ModEdge{From: from, To: to})
	}

	for _, m := range scan.Modules {
		for _, f := range m.Files {
			abs := filepath.Join(root, filepath.FromSlash(f.RelPath))
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			if len(data) > maxImportScanKB*1024 {
				data = data[:maxImportScanKB*1024]
			}
			src := string(data)
			for _, imp := range extractImports(src, f.RelPath) {
				if to, ok := resolveImportToModule(imp, pathByImport, modByPath); ok {
					addMesh(m.RelPath, to)
				}
			}
			if len(g.Mesh) >= maxMeshEdges {
				break
			}
		}
		if len(g.Mesh) >= maxMeshEdges {
			break
		}
	}
	sort.SliceStable(g.Mesh, func(i, j int) bool {
		if g.Mesh[i].From != g.Mesh[j].From {
			return g.Mesh[i].From < g.Mesh[j].From
		}
		return g.Mesh[i].To < g.Mesh[j].To
	})

	for _, m := range scan.Modules {
		names := map[string]bool{}
		var ordered []string
		for _, f := range m.Files {
			for _, fn := range f.Funcs {
				short := shortFuncName(fn.Name)
				if short == "" || names[short] {
					continue
				}
				names[short] = true
				ordered = append(ordered, short)
			}
		}
		fanIn := map[string]int{}
		var edges []FuncEdge
		edgeSeen := map[string]bool{}
		for _, f := range m.Files {
			abs := filepath.Join(root, filepath.FromSlash(f.RelPath))
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			if len(data) > maxImportScanKB*1024 {
				data = data[:maxImportScanKB*1024]
			}
			// Approximate: which funcs are called in this file; attribute to file's primary funcs
			callees := findCallees(string(data), names)
			callers := fileFuncNames(f)
			for _, caller := range callers {
				cs := shortFuncName(caller)
				for _, cal := range callees {
					if cal == cs {
						continue
					}
					k := cs + "->" + cal
					if edgeSeen[k] {
						continue
					}
					edgeSeen[k] = true
					edges = append(edges, FuncEdge{From: cs, To: cal})
					fanIn[cal]++
					if len(edges) >= maxSubnetEdges {
						break
					}
				}
				if len(edges) >= maxSubnetEdges {
					break
				}
			}
			if len(edges) >= maxSubnetEdges {
				break
			}
		}
		sort.SliceStable(edges, func(i, j int) bool {
			if edges[i].From != edges[j].From {
				return edges[i].From < edges[j].From
			}
			return edges[i].To < edges[j].To
		})
		g.Subnets[m.RelPath] = edges

		type hubScore struct {
			name  string
			score int
		}
		var hubs []hubScore
		for _, n := range ordered {
			hubs = append(hubs, hubScore{n, fanIn[n]})
		}
		sort.SliceStable(hubs, func(i, j int) bool {
			if hubs[i].score != hubs[j].score {
				return hubs[i].score > hubs[j].score
			}
			return hubs[i].name < hubs[j].name
		})
		var hubNames []string
		for i := 0; i < len(hubs) && i < maxHubsPerMod; i++ {
			hubNames = append(hubNames, hubs[i].name)
		}
		// Prefer ReadFirst file symbols if hubs empty
		if len(hubNames) == 0 && len(ordered) > 0 {
			lim := maxHubsPerMod
			if lim > len(ordered) {
				lim = len(ordered)
			}
			hubNames = ordered[:lim]
		}
		g.Hubs[m.RelPath] = hubNames
	}
	return g
}

func goModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func extractImports(src, rel string) []string {
	ext := strings.ToLower(filepath.Ext(rel))
	var out []string
	switch ext {
	case ".go":
		lines := strings.Split(src, "\n")
		inBlock := false
		for _, line := range lines {
			if m := reGoImportSingle.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
				continue
			}
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "import (") {
				inBlock = true
				continue
			}
			if inBlock {
				if trim == ")" {
					inBlock = false
					continue
				}
				if m := reGoImportLine.FindStringSubmatch(line); m != nil {
					out = append(out, m[1])
				}
			}
		}
	case ".ts", ".tsx", ".js", ".jsx":
		// import … from '…' / require('…')
		re := regexp.MustCompile(`(?:from|require\()\s*['"]([^'"]+)['"]`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out = append(out, m[1])
		}
	case ".py":
		re := regexp.MustCompile(`(?m)^\s*(?:from|import)\s+([a-zA-Z0-9_\.]+)`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out = append(out, strings.ReplaceAll(m[1], ".", "/"))
		}
	}
	return out
}

func resolveImportToModule(imp string, pathByImport map[string]string, mods map[string]*ModuleHit) (string, bool) {
	imp = strings.TrimSpace(imp)
	if imp == "" || strings.HasPrefix(imp, ".") {
		// relative: skip deep resolve for v1
		clean := filepath.ToSlash(imp)
		for p := range mods {
			if strings.Contains(clean, p) || strings.HasSuffix(clean, filepath.Base(p)) {
				return p, true
			}
		}
		return "", false
	}
	if p, ok := pathByImport[imp]; ok {
		return p, true
	}
	// suffix match: …/pkg/nav
	for p := range mods {
		if strings.HasSuffix(imp, "/"+p) || strings.HasSuffix(imp, p) {
			return p, true
		}
	}
	return "", false
}

func shortFuncName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Trim(name, "()*")
	return name
}

func fileFuncNames(f FileHit) []string {
	var out []string
	for _, fn := range f.Funcs {
		out = append(out, fn.Name)
	}
	return out
}

func findCallees(src string, known map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reFuncCall.FindAllStringSubmatch(src, -1) {
		n := m[1]
		if !known[n] || seen[n] {
			continue
		}
		// skip common noise
		switch n {
		case "Error", "String", "Printf", "Println", "Sprintf", "Fatal", "Fatalf", "New", "Make", "Append", "Len", "Close", "Read", "Write":
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func renderGraphMD(scan ScanResult, g GraphSnapshot, workspace string) string {
	var b strings.Builder
	totalFiles, totalFuncs := 0, 0
	for _, m := range scan.Modules {
		totalFiles += m.FileCount
		totalFuncs += m.FuncCount
	}
	fmt.Fprintf(&b, "# GRAPH — %s\n\n", scan.Label)
	fmt.Fprintf(&b, "> Neural map · %d modules · %d files · %d funcs · 0 LLM tokens  \n", len(scan.Modules), totalFiles, totalFuncs)
	fmt.Fprintf(&b, "> Workspace: ~/.am/workspaces/%s/  \n", workspace)
	b.WriteString("> AI: đọc **mesh + hubs + 1 subnet** — cấm dump toàn bộ. Chi tiết 1 func: `am map get`.\n\n")

	b.WriteString("## Mesh (module → module)\n\n")
	b.WriteString("### Sơ đồ nơ-ron (Mermaid — xem trên GitHub / IDE)\n\n")
	b.WriteString(renderMermaidMesh(g))
	b.WriteString("\n")
	b.WriteString("### Interactive\n\n")
	b.WriteString("```bash\nam map viz              # mở GRAPH.html (kéo node, click module → subnet)\nam map viz --module nav\n# hoặc proxy: http://127.0.0.1:8787/_am/map/viz?root=.\n```\n\n")
	b.WriteString("### Adjacency (text)\n\n")
	b.WriteString("```\n")
	if len(g.Mesh) == 0 {
		b.WriteString("(no import edges detected)\n")
	} else {
		var cur string
		var tos []string
		flush := func() {
			if cur == "" {
				return
			}
			fmt.Fprintf(&b, "%s → %s\n", shortMod(cur), strings.Join(mapShortMods(tos), " "))
		}
		for _, e := range g.Mesh {
			if e.From != cur {
				flush()
				cur = e.From
				tos = nil
			}
			tos = append(tos, e.To)
		}
		flush()
	}
	b.WriteString("```\n\n")

	b.WriteString("## Neurons (hubs)\n\n")
	b.WriteString("| module | files | funcs | hubs |\n")
	b.WriteString("|--------|------:|------:|------|\n")
	for _, path := range g.ModOrder {
		var m *ModuleHit
		for i := range scan.Modules {
			if scan.Modules[i].RelPath == path {
				m = &scan.Modules[i]
				break
			}
		}
		if m == nil {
			continue
		}
		hubs := g.Hubs[path]
		fmt.Fprintf(&b, "| `%s` | %d | %d | %s |\n",
			shortMod(path), m.FileCount, m.FuncCount, strings.Join(hubs, ", "))
	}
	b.WriteString("\n")

	b.WriteString("\n## Subnets\n\n")
	b.WriteString("Không nhúng hết vào đây (tiết kiệm token). Lấy 1 subnet:\n\n")
	b.WriteString("```bash\nam map graph <module>     # ví dụ: am map graph nav\nam map graph --list\n```\n")
	return b.String()
}

// RenderSubnetMD prints one module's func→func network (stdout / agent).
func RenderSubnetMD(scan ScanResult, g GraphSnapshot, mod string) (string, error) {
	mod = strings.TrimSpace(mod)
	mod = strings.TrimPrefix(mod, "pkg/")
	var path string
	for _, p := range g.ModOrder {
		if p == mod || shortMod(p) == mod || strings.HasSuffix(p, "/"+mod) {
			path = p
			break
		}
	}
	if path == "" {
		return "", fmt.Errorf("module %q not in graph (try: am map graph --list)", mod)
	}
	var m *ModuleHit
	for i := range scan.Modules {
		if scan.Modules[i].RelPath == path {
			m = &scan.Modules[i]
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Subnet — `%s`\n\n", shortMod(path))
	if m != nil {
		fmt.Fprintf(&b, "files=%d funcs=%d\n", m.FileCount, m.FuncCount)
	}
	hubs := g.Hubs[path]
	if len(hubs) > 0 {
		fmt.Fprintf(&b, "hubs: %s\n\n", strings.Join(hubs, ", "))
	}
	edges := g.Subnets[path]
	if len(edges) == 0 {
		b.WriteString("edges: *(sparse / leaf)*\n")
		return b.String(), nil
	}
	b.WriteString("```\n")
	var cur string
	var tos []string
	flush := func() {
		if cur == "" {
			return
		}
		fmt.Fprintf(&b, "%s → %s\n", cur, strings.Join(tos, " "))
	}
	for _, e := range edges {
		if e.From != cur {
			flush()
			cur = e.From
			tos = nil
		}
		tos = append(tos, e.To)
	}
	flush()
	b.WriteString("```\n")
	return b.String(), nil
}

// LoadGraphForDir rescans and builds graph (for CLI graph subcommand).
func LoadGraphForDir(startDir string) (ScanResult, GraphSnapshot, error) {
	root := GitRoot(startDir)
	if root == "" {
		return ScanResult{}, GraphSnapshot{}, fmt.Errorf("cannot resolve project directory")
	}
	scan, err := ScanProject(root)
	if err != nil {
		return ScanResult{}, GraphSnapshot{}, err
	}
	store, _ := LoadAnnotations(WorkspaceName(root))
	_ = ApplyAnnotations(&scan, store)
	return scan, buildGraph(root, scan), nil
}

func shortMod(path string) string {
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "pkg/") {
		return strings.TrimPrefix(path, "pkg/")
	}
	return path
}

func mapShortMods(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = shortMod(p)
	}
	return out
}
