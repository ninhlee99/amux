package cli

import (
	"fmt"

	"amux-accounts/pkg/identity"
)

// CmdMigrate executes non-destructive migration to the flat identity model.
func CmdMigrate(args []string) {
	fmt.Println("Migrating legacy accounts and profiles to flat Identity model...")
	count, err := identity.MigrateLegacyAccounts("", "")
	if err != nil {
		die("migration failed: %v", err)
	}
	if count == 0 {
		fmt.Println("No new legacy accounts to migrate. Configuration is up to date.")
		return
	}
	fmt.Printf("✓ Successfully migrated %d accounts to ~/.am/identities.json\n", count)
}
