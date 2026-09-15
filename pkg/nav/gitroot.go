package nav

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func execGitRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root, nil
	}
	return abs, nil
}

func gitRootOrSelf(dir string) string {
	root, err := execGitRoot(dir)
	if err != nil || root == "" {
		abs, e := filepath.Abs(dir)
		if e != nil {
			return dir
		}
		return abs
	}
	return root
}
