package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/mcp"
	"amux-accounts/pkg/types"
)

// CmdUpdate updates amux to the latest version from GitHub.
func CmdUpdate(args []string) {
	force := false
	quiet := false
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		}
		if a == "--quiet" || a == "-q" {
			quiet = true
		}
	}
	cmdUpdate(force, quiet)
}

// CmdUninstall removes amux hooks and binaries.
func CmdUninstall(args []string) {
	purge := false
	for _, a := range args {
		switch a {
		case "--purge", "--all":
			purge = true
		default:
			die("unknown option %q (usage: amux uninstall [--purge])", a)
		}
	}
	cmdUninstall(purge)
}

// cmdUninstall removes everything amux added outside ~/.amux, and only what
// it added: tool configs keep every value the user set, and the tools' own
// logins (Claude Code / Codex / Antigravity keychain items and files) stay
// exactly as they are, so each tool keeps working with its current account.
func cmdUninstall(purge bool) {
	fmt.Println("Uninstalling amux…")
	home, _ := os.UserHomeDir()
	step := func(ok bool, msg string, err error) {
		switch {
		case err != nil:
			fmt.Printf("  ⚠ %s: %v\n", msg, err)
		case ok:
			fmt.Printf("  ✓ %s\n", msg)
		}
	}

	hooked := hookedTools()
	step(len(hooked) > 0, "Unhooked "+strings.Join(hooked, ", "), gateway.Unhook(gateway.TargetAll))

	running := gateway.IsRunning()
	step(running, "Stopped the gateway", gateway.Stop())

	step(true, "Removed amux session hooks and status lines", hook.UninstallAllHooks())

	for _, t := range mcp.Targets() {
		if t.Installed(home) {
			_, err := t.Uninstall(home)
			step(true, "Removed MCP registration from "+t.Label, err)
		}
	}

	slash := filepath.Join(home, ".claude", "commands", "amux")
	if _, err := os.Stat(slash); err == nil {
		step(true, "Removed /amux slash commands", os.RemoveAll(slash))
	}

	if hook.IsAutoUpdateEnabled() {
		step(true, "Removed the auto-update LaunchAgent", hook.SetupAutoUpdate(false))
	}

	// Older versions exported these globally; drop them only if they still
	// point at the gateway (a value the user set is kept).
	if runtime.GOOS == "darwin" {
		for _, k := range []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL", "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_BASE", "GOOGLE_GENAI_BASE_URL"} {
			out, _ := exec.Command("launchctl", "getenv", k).Output()
			if v := strings.TrimSpace(string(out)); strings.Contains(v, "127.0.0.1:8787") || strings.Contains(v, "localhost:8787") {
				_ = exec.Command("launchctl", "unsetenv", k).Run()
				fmt.Printf("  ✓ Removed launchctl %s\n", k)
			}
		}
	}

	candidates := []string{
		filepath.Join(home, ".local", "bin", "amux"),
		filepath.Join(home, ".local", "bin", "am"),
		"/usr/local/bin/amux",
		"/usr/local/bin/am",
	}
	var selfInfo os.FileInfo
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			candidates = append(candidates, resolved)
			selfInfo, _ = os.Stat(resolved)
		}
	}
	seen := map[string]bool{}
	for _, p := range candidates {
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		// "am" is a common name: remove it only when it is this binary.
		if filepath.Base(p) == "am" {
			st, err := os.Stat(p)
			if err != nil || selfInfo == nil || !os.SameFile(st, selfInfo) {
				continue
			}
		}
		step(true, "Removed "+p, os.Remove(p))
	}

	if purge {
		step(true, "Deleted amux's master key from the keychain", auth.DeleteMasterKey())
		amuxDir := types.BaseDir()
		step(true, "Deleted "+amuxDir, os.RemoveAll(amuxDir))
		_ = os.RemoveAll(filepath.Join(home, ".am"))
		cleanGlobalEnvAndShellRC()
	} else {
		fmt.Printf("  • Kept %s (accounts and saved logins). Delete it too: amux uninstall --purge\n", types.BaseDir())
	}

	fmt.Println("Done. Claude Code, Codex, Cursor and Antigravity keep their current logins; restart open sessions.")
}

// tryUpdatePrebuilt downloads the latest pre-built binary from GitHub Releases.
// Returns true if the binary was successfully downloaded and installed.
func tryUpdatePrebuilt(targetBin string, quiet bool) bool {
	arch := runtime.GOARCH // "arm64" or "amd64"
	if arch != "arm64" && arch != "amd64" {
		return false
	}
	url := "https://github.com/ninhlee99/amux/releases/latest/download/amux-darwin-" + arch
	if !quiet {
		fmt.Printf("Trying pre-built binary: %s\n", url)
	}

	resp, err := http.Get(url) //nolint:gosec
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return false
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "amux-prebuilt-*")
	if err != nil {
		return false
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return false
	}
	tmp.Close()
	_ = os.Chmod(tmpName, 0o755)

	// Verify the downloaded binary works
	verifyCmd := exec.Command(tmpName, "version")
	verifyCmd.Stdout = io.Discard
	verifyCmd.Stderr = io.Discard
	if err := verifyCmd.Run(); err != nil {
		// Try "help" as fallback check
		verifyCmd2 := exec.Command(tmpName, "help")
		verifyCmd2.Stdout = io.Discard
		verifyCmd2.Stderr = io.Discard
		if err2 := verifyCmd2.Run(); err2 != nil {
			return false
		}
	}

	if err := copyExecutable(tmpName, targetBin); err != nil {
		return false
	}
	if !quiet {
		fmt.Printf("✓ Downloaded pre-built binary: %s\n", targetBin)
	}
	return true
}

func cmdUpdate(force, quiet bool) {
	if !quiet {
		fmt.Println("== Updating AMUX ==")
	}

	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(localBin, 0o755)
	targetBin := filepath.Join(localBin, "amux")

	// --- Try pre-built binary first (no git/go required) ---
	if tryUpdatePrebuilt(targetBin, quiet) {
		// Remove any stale legacy "am" binary
		_ = os.Remove(filepath.Join(localBin, "am"))
		_ = os.Remove("/usr/local/bin/am")
		if !quiet {
			fmt.Println("✓ Update complete! Identities and config preserved.")
		}
		return
	}
	if !quiet {
		fmt.Println("No pre-built binary found, falling back to source build...")
	}

	// --- Fall back to source build ---
	if !ensureGitAvailable(quiet) {
		if !quiet {
			die("git is required for source build update")
		}
		return
	}
	if !ensureGoAvailable(quiet) {
		if !quiet {
			die("go (>= 1.22) is required for source build update")
		}
		return
	}

	repoURL := "https://github.com/ninhlee99/amux.git"
	remoteCommit, _ := getRemoteHeadCommit(repoURL)
	localCommit := getInstalledCommit()

	if !force && remoteCommit != "" && localCommit != "" && localCommit == remoteCommit {
		if quiet {
			return
		}
		short := remoteCommit
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Printf("amux is already up to date (%s). Use 'amux update --force' to rebuild.\n", short)
		return
	}

	var buildDir string
	cwd, _ := os.Getwd()
	isLocalRepo := false
	if fi, err := os.Stat(filepath.Join(cwd, "main.go")); err == nil && !fi.IsDir() {
		if fi, err := os.Stat(filepath.Join(cwd, ".git")); err == nil && fi.IsDir() {
			isLocalRepo = true
		}
	}

	if isLocalRepo {
		buildDir = cwd
	} else {
		tmp, err := os.MkdirTemp("", "amux-update-*")
		if err != nil {
			if !quiet {
				die("create temp dir: %v", err)
			}
			return
		}
		defer os.RemoveAll(tmp)

		cloneCmd := exec.Command("git", "clone", "--depth", "1", repoURL, filepath.Join(tmp, "amux"))
		if err := cloneCmd.Run(); err != nil {
			if !quiet {
				die("clone repo: %v", err)
			}
			return
		}
		buildDir = filepath.Join(tmp, "amux")
	}

	tempBin := filepath.Join(os.TempDir(), fmt.Sprintf("amux-build-%d", time.Now().UnixNano()))
	buildCmd := exec.Command("go", "build", "-o", tempBin, "./cmd/amux")
	buildCmd.Dir = buildDir
	if err := buildCmd.Run(); err != nil {
		// Fallback build from root
		buildCmd = exec.Command("go", "build", "-o", tempBin, ".")
		buildCmd.Dir = buildDir
		if err := buildCmd.Run(); err != nil {
			if !quiet {
				die("build binary failed: %v", err)
			}
			return
		}
	}
	defer os.Remove(tempBin)

	if err := copyExecutable(tempBin, targetBin); err != nil {
		if !quiet {
			fmt.Printf("Warning: failed to write %s: %v\n", targetBin, err)
		}
	} else if !quiet {
		fmt.Printf("✓ Updated binary: %s\n", targetBin)
	}

	// Remove any stale legacy "am" binary alias to enforce single command surface
	_ = os.Remove(filepath.Join(localBin, "am"))
	_ = os.Remove("/usr/local/bin/am")

	if remoteCommit != "" {
		saveInstalledCommit(remoteCommit)
	} else if isLocalRepo {
		if out, err := exec.Command("git", "-C", cwd, "rev-parse", "HEAD").Output(); err == nil {
			saveInstalledCommit(strings.TrimSpace(string(out)))
		}
	}

	if !quiet {
		fmt.Println("✓ Update complete! Identities and config preserved.")
	}
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	_ = os.MkdirAll(dir, 0o755)

	tmpDst := filepath.Join(dir, fmt.Sprintf(".amux-tmp-%d", time.Now().UnixNano()))
	out, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmpDst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpDst)
		return err
	}
	_ = os.Chmod(tmpDst, 0o755)
	return os.Rename(tmpDst, dst)
}

type versionInfo struct {
	Commit    string `json:"commit"`
	UpdatedAt string `json:"updated_at"`
}

func getInstalledCommit() string {
	p := filepath.Join(types.BaseDir(), "version.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var vi versionInfo
	if json.Unmarshal(b, &vi) == nil {
		return vi.Commit
	}
	return ""
}

func saveInstalledCommit(commit string) {
	p := filepath.Join(types.BaseDir(), "version.json")
	vi := versionInfo{
		Commit:    commit,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	b, _ := json.MarshalIndent(vi, "", "  ")
	_ = os.WriteFile(p, b, 0o644)
}

func getRemoteHeadCommit(repoURL string) (string, error) {
	cmd := exec.Command("git", "ls-remote", repoURL, "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty response from git ls-remote")
	}
	return fields[0], nil
}

func ensureGitAvailable(quiet bool) bool {
	if _, err := exec.LookPath("git"); err == nil {
		return true
	}
	return false
}

func ensureGoAvailable(quiet bool) bool {
	if _, err := exec.LookPath("go"); err == nil {
		return true
	}
	return false
}
