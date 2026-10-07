package proxy

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// RunSupervisor keeps `bin args...` (the gateway daemon) alive:
//
//   - exit code 0 is only reachable through /_am/shutdown, i.e. a
//     deliberate `amux stop` — the supervisor exits cleanly.
//   - SIGTERM/SIGINT sent to the supervisor is forwarded to the child and
//     ends the loop, so `amux stop` can always take the whole tree down.
//   - any other exit is treated as a crash: respawn with exponential
//     backoff (reset once the child stayed up for supervisorStableUptime).
//     A crash loop never gives up — it just stays at the maximum backoff.
//
// It deliberately leaves client configs alone: native clients stay native
// and only `amux run` sandboxes point at the gateway.
func RunSupervisor(bin string, args ...string) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sig)

	var crashes []time.Time
	attempt := 0
	for {
		startedAt := time.Now()
		cmd := exec.Command(bin, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			log.Printf("amux supervisor: spawn failed: %v", err)
			select {
			case <-sig:
				return nil
			case <-time.After(superBackoff(attempt)):
			}
			attempt++
			continue
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		var waitErr error
		select {
		case waitErr = <-done:
		case <-sig:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			return nil
		}

		if waitErr == nil {
			log.Printf("amux supervisor: gateway exited cleanly, stopping")
			return nil
		}
		log.Printf("amux supervisor: gateway exited unexpectedly: %v", waitErr)

		now := time.Now()
		if now.Sub(startedAt) >= supervisorStableUptime {
			attempt = 0
			crashes = nil
		}
		crashes = append(crashes, now)
		if crashLooping(crashes, now) {
			log.Printf("amux supervisor: gateway is crash-looping (%d crashes in %s) — retrying at max backoff", len(crashes), supervisorCrashWindow)
			attempt = 1 << 10
		}

		select {
		case <-sig:
			return nil
		case <-time.After(superBackoff(attempt)):
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
