package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os/exec"
	"runtime"
)

// GeneratePKCE creates a cryptographically random code verifier and its
// corresponding SHA-256 code challenge (RFC 7636).
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// GenerateState generates a cryptographically random state string to mitigate CSRF attacks (RFC 6749 / OAuth 2.1).
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OpenBrowser opens the authorization URL in the user's default browser:
// - If the browser is not running, it launches the browser.
// - If the browser is already open, it opens a new tab.
func OpenBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// OpenPrivateBrowser is retained for compatibility and delegates to OpenBrowser.
func OpenPrivateBrowser(target string) (openedIncognito bool, browserName string, err error) {
	return false, "Default Browser", OpenBrowser(target)
}
