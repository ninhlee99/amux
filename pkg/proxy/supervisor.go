package proxy

import (
	"log"
	"os/exec"
	"strconv"
	"time"

	"amux-accounts/pkg/hook"
)

// RunSupervisor is what `am proxy --supervise` runs (spawned by
// CmdProxyUp instead of the bare server directly). It keeps a real `am
// proxy` server child alive:
//
//   - clean exit (code 0), only reachable via /_am/shutdown, i.e. a
//     deliberate `am proxy down` — the supervisor exits cleanly.
//   - when child is killed or exits unexpectedly: immediately triggers a
//     restart. If the child fails to become healthy within 5 seconds,
//     supervisor switches client configs (Claude / Codex / launchctl) back
//     to native subscription mode so the user can continue coding without
//     interruption.
//   - as soon as the proxy recovers and becomes healthy again, supervisor
//     automatically restores client configs to point back to the proxy.
//   - uses OS process lifecycle (cmd.Wait) — 0% CPU and 0 extra RAM while running.
func RunSupervisor(addr, upstream string, threshold float64) error {
	bin, err := resolveAMBin()
	if err != nil {
		return err
	}
	SetUsedThreshold(threshold)
	threshArg := strconv.FormatFloat(ParseUsedThreshold(threshold)*100, 'f', -1, 64)

	// Ensure client settings point to proxy initially
	_ = hook.SyncClientSettingsEnv(true, ProxyBase())

	attempt := 0
	subModeActive := false

	for {
		startedAt := time.Now()
		cmd := exec.Command(bin, "proxy",
			"--addr", addr,
			"--upstream", upstream,
			"--threshold", threshArg,
		)

		if err := cmd.Start(); err != nil {
			log.Printf("amux proxy supervisor: spawn failed: %v", err)
			if !subModeActive {
				_ = hook.SyncClientSettingsEnv(false, "")
				subModeActive = true
				log.Printf("amux proxy supervisor: proxy failed to start — switched client configs to native subscription")
			}
			time.Sleep(superBackoff(attempt))
			attempt++
			continue
		}

		// Wait for child to become reachable on addr (up to 5s)
		deadline := time.Now().Add(5 * time.Second)
		startedOk := false
		for time.Now().Before(deadline) {
			if ProxyUp() {
				startedOk = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if startedOk {
			if subModeActive {
				_ = hook.SyncClientSettingsEnv(true, ProxyBase())
				subModeActive = false
				log.Printf("amux proxy supervisor: proxy successfully restarted — restored client configs to proxy")
			}
			attempt = 0
		} else {
			// Failed to become reachable within 5s
			if !subModeActive {
				_ = hook.SyncClientSettingsEnv(false, "")
				subModeActive = true
				log.Printf("amux proxy supervisor: proxy not reachable within 5s — switched client configs to native subscription")
			}
		}

		// Block until the child process exits (event-driven via waitpid: 0% CPU, 0 extra RAM)
		waitErr := cmd.Wait()

		// Clean exit (exit code 0 via /_am/shutdown, i.e. deliberate `am proxy down`)
		if waitErr == nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 0 {
			log.Printf("amux proxy supervisor: server exited cleanly, stopping")
			return nil
		}

		log.Printf("amux proxy supervisor: server exited unexpectedly: %v", waitErr)

		// Child process was KILLED or CRASHED!
		// If child ran stably for a while before dying, reset attempt counter
		if time.Since(startedAt) >= supervisorStableUptime {
			attempt = 0
		}

		// If this is a repeat crash without stable uptime, back off slightly before respawn
		if attempt > 0 {
			time.Sleep(superBackoff(attempt))
		}
		attempt++
	}
}

const (
	supervisorBaseBackoff  = 500 * time.Millisecond
	supervisorMaxBackoff   = 8 * time.Second
	supervisorCrashWindow  = 60 * time.Second
	supervisorMaxCrashes   = 5
	supervisorStableUptime = 10 * time.Second
)

// superBackoff returns the delay before the (attempt+1)th respawn:
// 0.5s, 1s, 2s, 4s, 8s, 8s, ... — capped at supervisorMaxBackoff.
func superBackoff(attempt int) time.Duration {
	d := supervisorBaseBackoff
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= supervisorMaxBackoff {
			return supervisorMaxBackoff
		}
	}
	return d
}

// crashLooping reports whether history contains supervisorMaxCrashes or
// more entries within supervisorCrashWindow of now.
func crashLooping(history []time.Time, now time.Time) bool {
	count := 0
	for _, t := range history {
		if now.Sub(t) <= supervisorCrashWindow {
			count++
		}
	}
	return count >= supervisorMaxCrashes
}
