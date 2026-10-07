package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"amux-accounts/pkg/hook"
	"amux-accounts/pkg/profile"
)

func ProxyBase() string {
	return "http://" + ProxyAddr()
}

func ProxyUp() bool {
	c := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(ProxyBase() + "/_am/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func Sync() {
	if ProxyUp() {
		postAndClose(ProxyBase() + "/_am/sync")
	}
}

func postAndClose(url string) {
	resp, err := http.Post(url, "", nil)
	if err == nil && resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func attachedSessions() int {
	resp, err := http.Get(ProxyBase() + "/_am/status")
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	var s struct {
		Sessions int `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return -1
	}
	return s.Sessions
}

func CmdProxyDown(force, yesIKnow bool) {
	if !ProxyUp() {
		fmt.Println("amux proxy is not running")
		return
	}
	if force {
		if !yesIKnow {
			if n := attachedSessions(); n != 0 {
				if n < 0 {
					fmt.Fprintln(os.Stderr, "amux: couldn't confirm 0 claude tab(s) attached (status check failed) — "+
						"stopping now risks leaving one pointed at a dead port. "+
						"`amux proxy down --force --yes-i-know` to stop anyway.")
					return
				}
				fmt.Fprintf(os.Stderr, "amux: %d claude tab(s) still attached — stopping now leaves them pointed at a dead port "+
					"(ANTHROPIC_BASE_URL is fixed for the life of that process). "+
					"Close those tabs first, or `amux proxy down --force --yes-i-know` to stop anyway.\n", n)
				return
			}
		}
		postAndClose(ProxyBase() + "/_am/shutdown")
		_ = ClearAuthToken()
		hook.SyncLaunchctlEnv(false, "")
		if err := hook.SyncClientSettingsEnv(false, ""); err != nil {
			fmt.Fprintf(os.Stderr, "amux: sync client settings env: %v\n", err)
		}
		fmt.Println("amux proxy stopped")
		return
	}

	ppid := os.Getppid()
	if ppid > 1 {
		postAndClose(fmt.Sprintf("%s/_am/session?pid=%d&event=end&op=end", ProxyBase(), ppid))
		return
	}
	postAndClose(ProxyBase() + "/_am/shutdown")
	_ = ClearAuthToken()
	hook.SyncLaunchctlEnv(false, "")
	if err := hook.SyncClientSettingsEnv(false, ""); err != nil {
		fmt.Fprintf(os.Stderr, "amux: sync client settings env: %v\n", err)
	}
	fmt.Println("amux proxy stopped")
}

// CmdProxyDownPublic stops public proxy mode, clears public auth token,
// reverts persisted bind configuration back to local loopback (127.0.0.1),
// and stops the proxy daemon.
func CmdProxyDownPublic(force, yesIKnow bool) {
	_ = SaveBindPublic(false)
	_ = ClearAuthToken()

	if !ProxyUp() {
		fmt.Println("amux: public proxy disabled. Bind address reverted to 127.0.0.1 (local only).")
		return
	}

	CmdProxyDown(force, yesIKnow)
	fmt.Println("amux: public proxy stopped. Bind address reverted to 127.0.0.1 (local only). Public auth token revoked.")
}

// SwitchProfile installs a saved login of tool as the active one. For
// Claude with the gateway running, the switch goes through the gateway so
// its rotator snapshots the outgoing account and installs the new one in
// one place (doing both here and there would race on the keychain).
func SwitchProfile(tool, name string) error {
	if tool != "claude" || !ProxyUp() {
		return profile.CmdUse(tool, name)
	}
	resp, err := http.Post(ProxyBase()+"/_am/switch?to="+url.QueryEscape(name), "", nil)
	if err != nil {
		return fmt.Errorf("gateway switch: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	return nil
}

func CmdSwitchProvider(name string) {
	if !ProxyUp() {
		fmt.Fprintf(os.Stderr, "amux: proxy not running — start a session or run 'amux proxy' first\n")
		return
	}
	resp, err := http.Post(ProxyBase()+"/_am/switch-provider?to="+url.QueryEscape(name), "", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux: proxy switch: %v\n", err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "amux: proxy switch failed: %s\n", strings.TrimSpace(string(b)))
		return
	}
	fmt.Printf("switched active provider to %q (no restart needed)\n", name)
}
