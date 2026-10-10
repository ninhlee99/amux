package browser

import (
	"os"
	"path/filepath"

	"amux-accounts/pkg/types"
)

// ProfileExists reports whether ~/.amux/browser-profiles/<name> has been
// created (i.e. a login window was opened for it at least once).
func ProfileExists(name string) bool {
	entries, err := os.ReadDir(filepath.Join(types.BaseDir(), "browser-profiles", name))
	return err == nil && len(entries) > 0
}
