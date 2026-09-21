package cli

import (
	"fmt"
	"os"
	"strings"

	"amux-accounts/pkg/identity"
	"amux-accounts/pkg/project"
)

// CmdUse sets or shows the per-project identity binding in the current directory.
func CmdUse(args []string) {
	if len(args) == 0 {
		cmdProjectStatus()
		return
	}

	targetID := strings.TrimSpace(args[0])
	if targetID == "--clear" || targetID == "unset" || targetID == "rm" {
		_ = os.Remove(".amux")
		_ = os.Remove(".amux.json")
		fmt.Println("✓ Cleared local project identity binding. Falling back to global active account.")
		return
	}

	// Validate that targetID exists in identities
	cfg, err := identity.LoadConfig("")
	if err == nil && len(cfg.Identities) > 0 {
		found := false
		for _, id := range cfg.Identities {
			if id.ID == targetID {
				found = true
				break
			}
		}
		if !found {
			var available []string
			for _, id := range cfg.Identities {
				available = append(available, id.ID)
			}
			die("identity %q not found in local vault.\n  Available accounts: %s", targetID, strings.Join(available, ", "))
		}
	}

	cwd, _ := os.Getwd()
	projCfg := &project.Config{
		Account: targetID,
	}

	if err := project.SaveProjectConfig(cwd, projCfg); err != nil {
		die("failed to save project config: %v", err)
	}

	fmt.Printf("✓ Bound current directory to identity %q (saved in .amux).\n", targetID)
	fmt.Println("  Run 'eval \"$(amux env)\"' or 'amux switch' to activate this workspace context.")
}

// CmdProject manages project-level configuration.
func CmdProject(args []string) {
	if len(args) == 0 {
		cmdProjectStatus()
		return
	}

	switch args[0] {
	case "status", "info":
		cmdProjectStatus()
	case "set", "use":
		CmdUse(args[1:])
	case "clear", "unset", "rm":
		CmdUse([]string{"--clear"})
	default:
		CmdUse(args)
	}
}

func cmdProjectStatus() {
	cwd, _ := os.Getwd()
	loaded, path, err := project.LoadProjectConfig(cwd)
	if err != nil {
		die("load project config: %v", err)
	}

	fmt.Println("== AMUX Project Workspace ==")
	fmt.Printf("Working Directory: %s\n", cwd)
	if loaded != nil && loaded.Account != "" {
		fmt.Printf("Bound Identity:    %s (via %s)\n", loaded.Account, path)
		fmt.Println("\nTo switch project identity: 'amux use <account-id>'")
		fmt.Println("To clear project binding:   'amux use --clear'")
	} else {
		fmt.Println("Bound Identity:    (none - using global default)")
		fmt.Println("\nTo bind this project to a specific account:")
		fmt.Println("  amux use <account-id>")
	}
}
