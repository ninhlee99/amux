package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"amux-accounts/pkg/nav"
)

func cmdMap(args []string) {
	if len(args) == 0 {
		cmdMapShow("")
		return
	}
	switch args[0] {
	case "init", "scaffold", "generate":
		force := false
		rest := args[1:]
		for _, a := range rest {
			if a == "--force" || a == "-f" {
				force = true
			}
		}
		dir := "."
		for _, a := range rest {
			if a == "--force" || a == "-f" {
				continue
			}
			dir = a
			break
		}
		if !force {
			b := nav.Resolve(dir)
			if b.HasProjectMap() && fileExists(b.WorkspaceMap) {
				fmt.Printf("map already exists: %s\n", b.WorkspaceDir)
				fmt.Println("re-scan tree: am map update")
				cmdMapShow(dir)
				return
			}
		}
		b, err := nav.GenerateMap(dir, true)
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("map generated (local scan, 0 API tokens): %s\n", b.ProjectRoot)
		fmt.Printf("  workspace: %s\n", b.WorkspaceDir)
		fmt.Printf("  map:       %s\n", b.WorkspaceMap)
		fmt.Printf("  locate:    %s\n", b.WorkspaceLocate)
	case "update", "refresh", "resync":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		b, err := nav.UpdateMap(dir)
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("map updated: %s\n", b.WorkspaceDir)
		fmt.Printf("  map:    %s\n", b.WorkspaceMap)
		fmt.Printf("  locate: %s\n", b.WorkspaceLocate)
		if b.IsAmuxRepository() {
			fmt.Printf("  published: docs/GRAPH.md, docs/GRAPH.html, docs/MODULES.md (stub), docs/MAP_GENERATED.txt\n")
		}
	case "learn", "annotate", "enrich":
		cmdMapLearn(args[1:])
	case "recent", "touched", "check":
		cmdMapRecent(args[1:])
	case "touch", "note":
		cmdMapTouch(args[1:])
	case "get", "lookup":
		cmdMapGet(args[1:])
	case "graph", "mesh", "subnet":
		cmdMapGraph(args[1:])
	case "viz", "visual", "view":
		cmdMapViz(args[1:])
	case "show", "where", "paths":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		cmdMapShow(dir)
	case "json":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		b := nav.Resolve(dir)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(b)
	default:
		die("usage: am map [init|update|learn|recent|touch|get|graph|viz|show|json] …")
	}
}

func cmdMapViz(args []string) {
	dir, mod := ".", ""
	noOpen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--dir", "-C":
			dir = next()
		case "--module", "-m":
			mod = next()
		case "--no-open":
			noOpen = true
		default:
			if !strings.HasPrefix(a, "-") {
				if mod == "" {
					mod = a
				} else {
					dir = a
				}
			} else {
				die("usage: am map viz [--module MOD] [--no-open] [dir]")
			}
		}
	}
	path, b, err := nav.WriteGraphHTML(dir, mod)
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("neuron viz → %s\n", path)
	if b.IsAmuxRepository() {
		fmt.Printf("  published: %s\n", filepath.Join(b.ProjectRoot, "docs", nav.FileGraphHTML))
	}
	if !noOpen {
		u := "file://" + path
		if err := nav.OpenInBrowser(u); err != nil {
			fmt.Printf("open browser failed: %v\nopen: %s\n", err, path)
		}
	}
}

func cmdMapGraph(args []string) {
	dir := "."
	listOnly := false
	mod := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--dir", "-C":
			dir = next()
		case "--list", "-l":
			listOnly = true
		case "--module", "-m":
			mod = next()
		default:
			if !strings.HasPrefix(a, "-") {
				if mod == "" {
					mod = a
				} else {
					dir = a
				}
			} else {
				die("usage: am map graph <module>|--list [--dir DIR]")
			}
		}
	}
	scan, g, err := nav.LoadGraphForDir(dir)
	if err != nil {
		die("%v", err)
	}
	if listOnly || mod == "" {
		fmt.Println("modules:")
		for _, p := range g.ModOrder {
			hubs := g.Hubs[p]
			hub := ""
			if len(hubs) > 0 {
				hub = hubs[0]
			}
			fmt.Printf("  %-20s  files=%-3d funcs=%-4d hub=%s\n", p, moduleFileCount(scan, p), moduleFuncCount(scan, p), hub)
		}
		if mod == "" && !listOnly {
			fmt.Println("\nsubnet: am map graph <module>")
		}
		return
	}
	out, err := nav.RenderSubnetMD(scan, g, mod)
	if err != nil {
		die("%v", err)
	}
	fmt.Print(out)
}

func moduleFileCount(scan nav.ScanResult, path string) int {
	for _, m := range scan.Modules {
		if m.RelPath == path {
			return m.FileCount
		}
	}
	return 0
}

func moduleFuncCount(scan nav.ScanResult, path string) int {
	for _, m := range scan.Modules {
		if m.RelPath == path {
			return m.FuncCount
		}
	}
	return 0
}

func cmdMapLearn(args []string) {
	var (
		file, fn, summary, notes, dir string
		useStdin, useJSON             bool
		jsonRaw                       string
	)
	dir = "."
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--file", "-f":
			file = next()
		case "--func", "--fn", "-n":
			fn = next()
		case "--summary", "-s":
			summary = next()
		case "--notes":
			notes = next()
		case "--dir", "-C":
			dir = next()
		case "--stdin":
			useStdin = true
		case "--json":
			useJSON = true
			jsonRaw = next()
		default:
			if !strings.HasPrefix(a, "-") && dir == "." && file == "" {
				dir = a
			} else {
				die("unknown learn flag %q\nusage: am map learn --file PATH --func NAME --summary TEXT\n   or: am map learn --stdin < annotations.json", a)
			}
		}
	}

	var anns []nav.FuncAnnotation
	switch {
	case useStdin:
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			die("read stdin: %v", err)
		}
		anns, err = parseLearnJSON(data)
		if err != nil {
			die("%v", err)
		}
	case useJSON:
		var err error
		anns, err = parseLearnJSON([]byte(jsonRaw))
		if err != nil {
			die("%v", err)
		}
	default:
		anns = []nav.FuncAnnotation{{
			File:    file,
			Func:    fn,
			Summary: summary,
			Notes:   notes,
			Source:  "agent",
		}}
	}

	b, n, err := nav.LearnFuncs(dir, anns)
	if err != nil {
		die("%v", err)
	}
	var touches []nav.FocusFuncRef
	for _, a := range anns {
		touches = append(touches, nav.FocusFuncRef{File: a.File, Func: a.Func})
	}
	_, _ = nav.NoteTouch(dir, nil, touches)
	fmt.Printf("learned %d annotation(s) → %s\n", n, filepath.Join(b.WorkspaceDir, nav.FileAnnotations))
	fmt.Printf("(check/update: recent+touched only — không đọc full MODULES)\n")
}

func cmdMapRecent(args []string) {
	dir, since := ".", "HEAD"
	needsOnly, asJSON := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--dir", "-C":
			dir = next()
		case "--since":
			since = next()
		case "--needs-learn", "--stale":
			needsOnly = true
		case "--json":
			asJSON = true
		default:
			if !strings.HasPrefix(a, "-") {
				dir = a
			} else {
				die("usage: am map recent [--since HEAD] [--needs-learn] [--json] [dir]")
			}
		}
	}
	rep, err := nav.RecentFocus(dir, since, needsOnly)
	if err != nil {
		die("%v", err)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}
	fmt.Printf("focus check (git∪touched) since=%s → %d func(s) in %d file(s)\n", rep.Since, rep.Count, len(rep.Files))
	if rep.Count == 0 {
		fmt.Println("(empty — không cần đọc map/repo)")
		return
	}
	for _, h := range rep.Funcs {
		flag := "ok"
		if h.NeedsLearn {
			flag = "LEARN"
		}
		fmt.Printf("  [%s] %s:%d %s — %s (%s)\n", flag, h.File, h.Line, h.Func, truncateRunes(h.Summary, 80), h.Reason)
	}
}

func cmdMapTouch(args []string) {
	dir, file, fn := ".", "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--dir", "-C":
			dir = next()
		case "--file", "-f":
			file = next()
		case "--func", "--fn", "-n":
			fn = next()
		default:
			die("usage: am map touch --file PATH [--func NAME]")
		}
	}
	var funcs []nav.FocusFuncRef
	var files []string
	if file != "" && fn != "" {
		funcs = append(funcs, nav.FocusFuncRef{File: file, Func: fn})
	} else if file != "" {
		files = append(files, file)
	} else {
		die("usage: am map touch --file PATH [--func NAME]")
	}
	s, err := nav.NoteTouch(dir, files, funcs)
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("touched %d file(s), %d func(s) → focus.json\n", len(s.Files), len(s.Funcs))
}

func cmdMapGet(args []string) {
	dir, file, fn := ".", "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				die("missing value after %s", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--dir", "-C":
			dir = next()
		case "--file", "-f":
			file = next()
		case "--func", "--fn", "-n":
			fn = next()
		default:
			die("usage: am map get --file PATH --func NAME")
		}
	}
	h, err := nav.LookupFunc(dir, file, fn)
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("%s:%d %s\n%s\n", h.File, h.Line, h.Func, h.Summary)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func parseLearnJSON(data []byte) ([]nav.FuncAnnotation, error) {
	data = bytesTrim(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty json")
	}
	// Accept single object or array
	if data[0] == '{' {
		var one nav.FuncAnnotation
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, err
		}
		return []nav.FuncAnnotation{one}, nil
	}
	var many []nav.FuncAnnotation
	if err := json.Unmarshal(data, &many); err != nil {
		return nil, err
	}
	return many, nil
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func cmdMapShow(dir string) {
	b := nav.Resolve(dir)
	if b.ProjectRoot == "" {
		die("cannot resolve project root from %q", dir)
	}
	fmt.Printf("project:   %s (%s)\n", b.Label, b.ProjectRoot)
	fmt.Printf("workspace: %s\n", b.WorkspaceDir)
	if b.IsAmuxRepository() {
		fmt.Println("kind:      amux tool repo → dùng docs/AI_CODEBASE_MAP.md của amux")
	} else {
		fmt.Println("kind:      client project → ~/.am/workspaces/<name>/")
	}
	fmt.Println()
	fmt.Println("navigation files:")
	printPath("  map (primary)", b.PrimaryMap)
	printPath("  locate (primary)", b.PrimaryLocate)
	printPath("  workspace map", b.WorkspaceMap)
	printPath("  workspace locate", b.WorkspaceLocate)
	printPath("  graph (neural)", filepath.Join(b.WorkspaceDir, nav.FileGraphMD))
	printPath("  graph viz (html)", filepath.Join(b.WorkspaceDir, nav.FileGraphHTML))
	printPath("  modules stub", filepath.Join(b.WorkspaceDir, nav.FileModulesMD))
	printPath("  learned annotations", filepath.Join(b.WorkspaceDir, nav.FileAnnotations))
	printPath("  session focus", filepath.Join(b.WorkspaceDir, nav.FileFocus))
	if !b.HasProjectMap() {
		fmt.Println()
		fmt.Println("chưa có bản đồ — chạy: am map init")
	}
}

func printPath(label, p string) {
	if p == "" {
		return
	}
	mark := "·"
	if _, err := os.Stat(p); err == nil {
		mark = "✓"
	}
	fmt.Printf("%s %s  %s\n", mark, label, p)
}
