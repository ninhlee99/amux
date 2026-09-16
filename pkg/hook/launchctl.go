package hook

import (
	"os/exec"
	"runtime"
)

// LaunchctlSetenv is disabled to avoid polluting macOS global GUI session environment.
func LaunchctlSetenv(key, val string) error {
	return nil
}

// LaunchctlUnsetenv removes a var set via launchctl. No-op off darwin.
func LaunchctlUnsetenv(key string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return exec.Command("launchctl", "unsetenv", key).Run()
}

// SyncLaunchctlEnv cleans up any legacy vars from launchctl if proxy is down,
// and intentionally does not set global session env vars when proxy is up.
func SyncLaunchctlEnv(proxyUp bool, proxyBase string) {
	if proxyUp {
		return
	}
	_ = LaunchctlUnsetenv("ANTHROPIC_BASE_URL")
	_ = LaunchctlUnsetenv("ANTHROPIC_AUTH_TOKEN")
	_ = LaunchctlUnsetenv("OPENAI_BASE_URL")
	_ = LaunchctlUnsetenv("OPENAI_API_KEY")
	_ = LaunchctlUnsetenv("GEMINI_API_BASE")
	_ = LaunchctlUnsetenv("GOOGLE_GENAI_BASE_URL")
	_ = LaunchctlUnsetenv("GOOGLE_GEMINI_BASE_URL")
	_ = LaunchctlUnsetenv("GEMINI_API_KEY")
	_ = LaunchctlUnsetenv("GOOGLE_GENAI_API_KEY")
	_ = LaunchctlUnsetenv("GOOGLE_API_KEY")
}
