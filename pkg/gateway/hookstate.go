package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"

	"amux-accounts/pkg/types"
)

// Hook state records what a hook added that cannot be recognised later from
// the value alone (e.g. a setting amux filled in only because it was empty),
// so unhook removes exactly that and nothing the user set.

func hookStatePath() string { return filepath.Join(types.BaseDir(), "hook-state.json") }

func loadHookState() map[string]bool {
	m := map[string]bool{}
	if b, err := os.ReadFile(hookStatePath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func saveHookState(m map[string]bool) {
	if len(m) == 0 {
		_ = os.Remove(hookStatePath())
		return
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	_ = os.MkdirAll(filepath.Dir(hookStatePath()), 0o700)
	_ = os.WriteFile(hookStatePath(), append(b, '\n'), 0o600)
}

func setHookState(key string, v bool) {
	m := loadHookState()
	m[key] = v
	saveHookState(m)
}

// hookState returns the recorded value and whether one was recorded.
func hookState(key string) (bool, bool) {
	v, ok := loadHookState()[key]
	return v, ok
}

func clearHookState(key string) {
	m := loadHookState()
	if _, ok := m[key]; ok {
		delete(m, key)
		saveHookState(m)
	}
}
