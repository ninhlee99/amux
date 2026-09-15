package nav

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceNameStable(t *testing.T) {
	t.Setenv("AMUX_HOME", t.TempDir())
	n1 := WorkspaceName("/Users/foo/myapp")
	n2 := WorkspaceName("/Users/foo/myapp")
	if n1 != n2 {
		t.Fatalf("unstable: %q vs %q", n1, n2)
	}
	if n1 != "myapp" {
		t.Fatalf("want myapp, got %q", n1)
	}
}

func TestWorkspaceNameCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	dir := WorkspaceDir("web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileRootMeta), []byte("/other/web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := WorkspaceName("/Users/foo/web")
	if got == "web" {
		t.Fatal("collision should disambiguate")
	}
	if !strings.HasPrefix(got, "web-") {
		t.Fatalf("want web-<hash>, got %q", got)
	}
}

func TestGenerateMapFromScan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	// Mini Go-like tree
	pkg := filepath.Join(root, "pkg", "auth")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package auth

// Login authenticates the user and returns a session error if any.
func Login() error { return nil }

type Session struct{}
`
	if err := os.WriteFile(filepath.Join(pkg, "session.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := GenerateMap(root, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(b.WorkspaceLocate)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "pkg-auth") && !strings.Contains(body, "auth") {
		t.Fatalf("locate missing auth module: %s", body)
	}
	if !strings.Contains(body, "Login") && !strings.Contains(body, "Session") {
		t.Fatalf("expected symbols from scan: %s", body)
	}
	if !strings.Contains(body, "file_count") || !strings.Contains(body, "func_count") {
		t.Fatalf("expected file/func counts: %s", body)
	}
	graphPath := filepath.Join(b.WorkspaceDir, FileGraphMD)
	graphBody, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(graphBody), "## Mesh") || !strings.Contains(string(graphBody), "## Neurons") {
		t.Fatalf("GRAPH.md missing mesh/neurons: %s", graphBody)
	}
	if !strings.Contains(string(graphBody), "Login") {
		t.Fatalf("expected Login hub/subnet in GRAPH: %s", graphBody)
	}
	modPath := filepath.Join(b.WorkspaceDir, FileModulesMD)
	modBody, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modBody), "GRAPH.md") || !strings.Contains(string(modBody), "stub") {
		t.Fatalf("MODULES.md should be stub pointing to GRAPH: %s", modBody)
	}
}

func TestUpdateMapRegenerates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644)
	b1, err := GenerateMap(root, true)
	if err != nil {
		t.Fatal(err)
	}
	// add module then update
	dir := filepath.Join(root, "pkg", "billing")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bill.go"), []byte("package billing\nfunc Charge() {}\n"), 0o644)
	b2, err := UpdateMap(root)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(b2.WorkspaceLocate)
	if !strings.Contains(string(data), "billing") {
		t.Fatalf("update should pick new module: %s", data)
	}
	if b1.WorkspaceDir != b2.WorkspaceDir {
		t.Fatal("workspace path should stay stable")
	}
}

func TestLearnFuncsOverlaysSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg", "auth")
	_ = os.MkdirAll(pkg, 0o755)
	_ = os.WriteFile(filepath.Join(pkg, "login.go"), []byte("package auth\n\nfunc Login() error { return nil }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\n"), 0o644)

	if _, err := GenerateMap(root, true); err != nil {
		t.Fatal(err)
	}
	_, n, err := LearnFuncs(root, []FuncAnnotation{{
		File:    "pkg/auth/login.go",
		Func:    "Login",
		Summary: "Validates password hash then issues JWT",
		Source:  "agent",
	}})
	if err != nil || n != 1 {
		t.Fatalf("learn: n=%d err=%v", n, err)
	}
	hit, err := LookupFunc(root, "pkg/auth/login.go", "Login")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hit.Summary, "[learned]") || !strings.Contains(hit.Summary, "JWT") {
		t.Fatalf("lookup should show learned summary: %+v", hit)
	}
	// update must keep annotation
	if _, err := UpdateMap(root); err != nil {
		t.Fatal(err)
	}
	hit2, err := LookupFunc(root, "pkg/auth/login.go", "Login")
	if err != nil || !strings.Contains(hit2.Summary, "JWT") {
		t.Fatalf("annotation lost after update: %+v err=%v", hit2, err)
	}
}

func TestParseDiffHunks(t *testing.T) {
	diff := []byte("+++ b/pkg/auth/login.go\n@@ -1,0 +3,2 @@\n+func Login() {}\n")
	h := parseDiffHunks(diff)
	rs := h["pkg/auth/login.go"]
	if len(rs) != 1 || rs[0].start != 3 {
		t.Fatalf("hunks=%v", rs)
	}
}

func TestRecentFocusTouchedOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg", "auth")
	_ = os.MkdirAll(pkg, 0o755)
	_ = os.WriteFile(filepath.Join(pkg, "login.go"), []byte("package auth\n\nfunc Login() error { return nil }\nfunc Other() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\n"), 0o644)
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")

	if _, err := GenerateMap(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NoteTouch(root, nil, []FocusFuncRef{{File: "pkg/auth/login.go", Func: "Login"}}); err != nil {
		t.Fatal(err)
	}
	rep, err := RecentFocus(root, "HEAD", false)
	if err != nil {
		t.Fatal(err)
	}
	foundLogin, foundOther := false, false
	for _, h := range rep.Funcs {
		if h.Func == "Login" {
			foundLogin = true
		}
		if h.Func == "Other" {
			foundOther = true
		}
	}
	if !foundLogin {
		t.Fatalf("expected touched Login in report: %+v", rep.Funcs)
	}
	if foundOther {
		t.Fatalf("Other should not appear without git/focus: %+v", rep.Funcs)
	}

	h, err := LookupFunc(root, "pkg/auth/login.go", "Login")
	if err != nil || h.Line == 0 {
		t.Fatalf("lookup: %+v err=%v", h, err)
	}
}

func TestPublishAmuxInventoryPortableRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "pkg", "cli"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module amux-accounts\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "cli", "cli.go"), []byte("package cli\nfunc Run() {}\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	if _, err := GenerateMap(root, true); err != nil {
		t.Fatal(err)
	}
	gen, err := os.ReadFile(filepath.Join(root, "docs", "MAP_GENERATED.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(gen)
	if !strings.Contains(body, "root=.") {
		t.Fatalf("want portable root=.: %s", body)
	}
	if strings.Contains(body, root) {
		t.Fatalf("docs must not embed absolute path %q: %s", root, body)
	}
	mod, err := os.ReadFile(filepath.Join(root, "docs", FileModulesMD))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), root) {
		t.Fatalf("MODULES.md must not embed absolute path: %s", mod)
	}
	if !strings.Contains(string(mod), "GRAPH.md") {
		t.Fatalf("want MODULES stub → GRAPH: %s", mod)
	}
	graph, err := os.ReadFile(filepath.Join(root, "docs", FileGraphMD))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(graph), root) {
		t.Fatalf("GRAPH.md must not embed absolute path: %s", graph)
	}
	if !strings.Contains(string(graph), "## Mesh") {
		t.Fatalf("GRAPH missing mesh: %s", graph)
	}
}

func TestRenderGraphHTMLAndMermaid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AMUX_HOME", home)
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "pkg", "a"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "pkg", "b"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "a", "a.go"), []byte("package a\nimport \"example.com/demo/pkg/b\"\nfunc A() { b.B() }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "b", "b.go"), []byte("package b\nfunc B() {}\n"), 0o644)

	scan, err := ScanProject(root)
	if err != nil {
		t.Fatal(err)
	}
	g := buildGraph(root, scan)
	if len(g.Mesh) == 0 {
		t.Fatalf("expected mesh edge a→b: %+v", g.Mesh)
	}
	md := renderGraphMD(scan, g, "demo")
	if !strings.Contains(md, "```mermaid") || !strings.Contains(md, "-->") {
		t.Fatalf("GRAPH.md should include mermaid arrows: %s", md)
	}
	html, err := RenderGraphHTML(scan, g, "")
	if err != nil || !strings.Contains(string(html), "canvas") || !strings.Contains(string(html), `"mesh"`) {
		t.Fatalf("HTML viz missing canvas/mesh: err=%v len=%d", err, len(html))
	}
}

func TestResolveAmuxRepo(t *testing.T) {
	wd, _ := os.Getwd()
	root := GitRoot(wd)
	if root == "" {
		t.Skip("no git root")
	}
	b := Resolve(root)
	if !fileExists(filepath.Join(root, "go.mod")) {
		t.Skip("not in go module tree")
	}
	data, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if !strings.Contains(string(data), "amux-accounts") {
		t.Skip("not amux repo")
	}
	if !b.IsAmuxRepository() {
		t.Fatal("should detect amux repo")
	}
}

func TestBundleAsPathsOnly(t *testing.T) {
	b := Bundle{
		ProjectRoot:   "/tmp/demo",
		Workspace:     "demo",
		WorkspaceDir:  "/home/.am/workspaces/demo",
		PrimaryMap:    "/home/.am/workspaces/demo/AI_CODEBASE_MAP.md",
		PrimaryLocate: "/home/.am/workspaces/demo/ai-locate.yaml",
		MapPath:       "/tmp/demo/docs/MAP.md",
		LocatePath:    "/tmp/demo/docs/locate.yaml",
		AgentsPath:    "/tmp/demo/AGENTS.md",
	}
	p := b.AsPathsOnly()
	if p.ProjectRoot != b.ProjectRoot || p.PrimaryMap != b.PrimaryMap {
		t.Fatalf("paths mismatch: %+v", p)
	}
	if p.Hint == "" || !strings.Contains(p.Hint, "Never dump") {
		t.Fatalf("missing refuse-dump hint: %q", p.Hint)
	}
	// Must not expose every legacy path field — keep agent payload small.
	if p.WorkspaceDir == "" {
		t.Fatal("want workspace_dir")
	}
}
