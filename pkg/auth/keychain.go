package auth

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"amux-accounts/pkg/types"
)

// KCTimeout bounds every `security` invocation so it can never block a
// caller indefinitely — the original bug was a detached background process
// stalling forever behind a Keychain access prompt nobody could answer.
// It applies to interactive callers too (amux id add, amux use, ...), so
// it's set generously enough to not cut off a user who's mid-prompt
// approving Touch ID / their login password, while still bounding the
// worst case. Exported because gateway.Start()'s readiness deadline must
// stay comfortably above this: gateway startup runs a keychain lookup
// synchronously before it can bind its listener, so a deadline shorter
// than (or too close to) this timeout would misreport a healthy-but-slow
// startup as a failure.
const KCTimeout = 10 * time.Second

// KCGet retrieves a password item from macOS Keychain.
func KCGet(service, account string) (string, error) {
	args := []string{"find-generic-password", "-s", service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	ctx, cancel := context.WithTimeout(context.Background(), KCTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("security: timed out after %s (keychain may be waiting on a prompt)", KCTimeout)
		}
		return "", fmt.Errorf("security: %s", strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// KCAccount reads the existing item's account attribute, if any.
func KCAccount(service string) string {
	ctx, cancel := context.WithTimeout(context.Background(), KCTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", service)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.Contains(ln, `"acct"`) {
			if i := strings.Index(ln, `="`); i >= 0 {
				return strings.TrimSuffix(ln[i+2:], `"`)
			}
		}
	}
	return ""
}

// KCSet writes or updates a generic-password item in macOS Keychain.
func KCSet(service, account, secret string) error {
	if account == "" {
		if a := KCAccount(service); a != "" {
			account = a
		} else {
			account = types.CurrentUser()
		}
	}
	// -X takes the password as a hex string, sidestepping argv quoting issues.
	args := []string{
		"add-generic-password", "-U",
		"-s", service, "-a", account,
		"-X", hex.EncodeToString([]byte(secret)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), KCTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("security add: timed out after %s (keychain may be waiting on a prompt)", KCTimeout)
		}
		return fmt.Errorf("security add: %s", strings.TrimSpace(errb.String()))
	}
	return nil
}
