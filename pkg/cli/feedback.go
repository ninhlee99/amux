package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"amux-accounts/pkg/identity"
)

const feedbackRepo = "ninhlee99/amux"

// CmdFeedback handles creating an issue on GitHub for bug reports or feature ideas.
func CmdFeedback(args []string) {
	kind := "bug"
	var titleWords []string
	for _, a := range args {
		switch a {
		case "-b", "--bug":
			kind = "bug"
		case "-i", "--idea":
			kind = "idea"
		default:
			titleWords = append(titleWords, a)
		}
	}
	title := strings.Join(titleWords, " ")
	if title == "" {
		fmt.Print("short title for the issue: ")
		r := bufio.NewReader(os.Stdin)
		line, _ := r.ReadString('\n')
		title = strings.TrimSpace(line)
		if title == "" {
			die("cancelled (no title given)")
		}
	}

	fmt.Println("describe what happened / what you'd like — blank line to finish:")
	sc := bufio.NewScanner(os.Stdin)
	var lines []string
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) == "" {
			break
		}
		lines = append(lines, l)
	}
	body := strings.Join(lines, "\n")

	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("---\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	if cfg, err := identity.LoadConfig(""); err == nil && cfg != nil {
		var activeList []string
		for _, id := range cfg.Identities {
			if id.Active {
				activeList = append(activeList, fmt.Sprintf("%s (%s)", id.ID, id.Provider))
			}
		}
		if len(activeList) > 0 {
			fmt.Fprintf(&b, "active identities: %s\n", strings.Join(activeList, ", "))
		}
	}

	label := "bug"
	if kind == "idea" {
		label = "enhancement"
	}

	ghPath, err := exec.LookPath("gh")
	if err == nil {
		cmd := exec.Command(ghPath, "issue", "create", "-R", feedbackRepo, "-t", title, "-b", b.String(), "-l", label)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "amux: gh issue create failed (%v) — opening browser instead\n", err)
		} else {
			return
		}
	}

	u := fmt.Sprintf("https://github.com/%s/issues/new?title=%s&body=%s",
		feedbackRepo, url.QueryEscape(title), url.QueryEscape(b.String()))
	fmt.Println("opening:", u)
	var openCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		openCmd = exec.Command("open", u)
	case "linux":
		openCmd = exec.Command("xdg-open", u)
	default:
		fmt.Println("open that URL in a browser to file the issue.")
		return
	}
	if err := openCmd.Start(); err != nil {
		fmt.Println("couldn't launch a browser — open the URL above manually.")
	}
}
