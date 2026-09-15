package hook

import (
	"os/exec"
	"runtime"
)

// LaunchctlSetenv mirrors a var into the macOS GUI session (launchctl
// setenv), so apps launched outside any shell — Dock icons, IDE
// integrations, editor extensions — inherit it too, not just processes
// spawned from a shell that sourced `eval "$(am env)"`. No-op off darwin.
func LaunchctlSetenv(key, val string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return exec.Command("launchctl", "setenv", key, val).Run()
}

// LaunchctlUnsetenv removes a var set via LaunchctlSetenv. No-op off darwin.
func LaunchctlUnsetenv(key string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return exec.Command("launchctl", "unsetenv", key).Run()
}

// SyncLaunchctlEnv mirrors the proxy's current reachability into the
// launchctl session env, so GUI-launched processes (which never source
// shell rc, see HookInstall) fall back to the real Anthropic API the same
// way a fresh shell does via `am env` — instead of staying pointed at a
// dead local port once the proxy goes down.
//
// proxyUp true  -> setenv Anthropic/OpenAI/Gemini gateway BASE + dummy keys
//                 at proxyBase. Gemini-only keys (GEMINI_API_KEY /
//                 GOOGLE_GENAI_API_KEY), never GOOGLE_API_KEY (Maps/gcloud).
// proxyUp false -> unsetenv those, plus leftover GOOGLE_API_KEY.
func SyncLaunchctlEnv(proxyUp bool, proxyBase string) {
	if proxyUp {
		_ = LaunchctlSetenv("ANTHROPIC_BASE_URL", proxyBase)
		_ = LaunchctlSetenv("ANTHROPIC_AUTH_TOKEN", "am-proxy")
		_ = LaunchctlSetenv("OPENAI_BASE_URL", proxyBase+"/v1")
		_ = LaunchctlSetenv("OPENAI_API_KEY", "am-proxy")
		_ = LaunchctlSetenv("GEMINI_API_BASE", proxyBase)
		_ = LaunchctlSetenv("GOOGLE_GENAI_BASE_URL", proxyBase)
		_ = LaunchctlSetenv("GOOGLE_GEMINI_BASE_URL", proxyBase)
		_ = LaunchctlSetenv("GEMINI_API_KEY", "am-proxy")
		_ = LaunchctlSetenv("GOOGLE_GENAI_API_KEY", "am-proxy")
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
