package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/proxy"
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
		if a == "--purge" || a == "--all" {
			purge = true
		}
	}
	cmdUninstall(purge)
}

func cmdUninstall(purge bool) {
	fmt.Println("== Uninstalling AMUX ==")

	if proxy.ProxyUp() {
		fmt.Println("Stopping gateway daemon...")
		proxy.CmdProxyDown(true, true)
	}

	fmt.Println("Removing hooks from IDEs...")
	_ = hook.UninstallAllHooks()

	if hook.IsAutoUpdateEnabled() {
		fmt.Println("Disabling auto-update LaunchAgent...")
		_ = hook.SetupAutoUpdate(false)
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "amux"),
		filepath.Join(home, ".local", "bin", "am"),
		"/usr/local/bin/amux",
		"/usr/local/bin/am",
	}
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			candidates = append(candidates, resolved)
		}
	}

	for _, p := range candidates {
		if fi, err := os.Lstat(p); err == nil {
			if err := os.Remove(p); err == nil {
				fmt.Printf("Removed binary: %s\n", p)
			} else {
				fmt.Printf("Could not remove %s: %v\n", p, err)
			}
			_ = fi
		}
	}

	if purge {
		amDir := filepath.Join(home, ".am")
		if err := os.RemoveAll(amDir); err != nil {
			fmt.Printf("Could not remove %s: %v\n", amDir, err)
		} else {
			fmt.Printf("Purged configuration directory: %s\n", amDir)
		}
	} else {
		fmt.Printf("Identity data at ~/.am/ preserved (use --purge to delete).\n")
	}

	fmt.Println("Uninstall complete.")
}

func cmdUpdate(force, quiet bool) {
	if !quiet {
		fmt.Println("== Updating AMUX ==")
	}
	if !ensureGitAvailable(quiet) {
		if !quiet {
			die("git is required for update")
		}
		return
	}
	if !ensureGoAvailable(quiet) {
		if !quiet {
			die("go (>= 1.22) is required for update")
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

	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(localBin, 0o755)

	targetBin := filepath.Join(localBin, "amux")
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
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".am", "version.json")
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
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".am", "version.json")
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
