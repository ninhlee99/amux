package cli

import (
	"fmt"
	"os"

	"amux-accounts/pkg/env"
	"amux-accounts/pkg/gateway"
	"amux-accounts/pkg/project"
)

// CmdEnv handles shell environment exports and env variable management for eval "$(amux env)".
func CmdEnv(args []string) {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--public" || args[0] == "-p")) {
		base := "http://127.0.0.1:8787"
		proxyUp := gateway.IsRunning()
		env.PrintEnvExports(proxyUp, true, base)

		// Check if project has local identity binding
		if proj, path, err := project.LoadProjectConfig(""); err == nil && proj != nil && proj.Account != "" {
			fmt.Printf("# Project identity bound via %s\n", path)
			fmt.Printf("export AMUX_PROJECT_IDENTITY=%s\n", proj.Account)
		}
		return
	}

	switch args[0] {
	case "set":
		if len(args) < 3 {
			die("usage: amux env set KEY VALUE")
		}
		m := env.LoadEnvVars()
		m[args[1]] = args[2]
		if err := env.SaveEnvVars(m); err != nil {
			die("save env: %v", err)
		}
		fmt.Fprintf(os.Stderr, "set %s\n", args[1])
	case "get":
		if len(args) < 2 {
			die("usage: amux env get KEY")
		}
		v, ok := env.LoadEnvVars()[args[1]]
		if !ok {
			die("%s not set", args[1])
		}
		fmt.Println(v)
	case "rm", "unset":
		if len(args) < 2 {
			die("usage: amux env rm KEY")
		}
		m := env.LoadEnvVars()
		delete(m, args[1])
		if err := env.SaveEnvVars(m); err != nil {
			die("save env: %v", err)
		}
		fmt.Fprintf(os.Stderr, "removed %s\n", args[1])
	case "list", "ls":
		m := env.LoadEnvVars()
		for k, v := range m {
			fmt.Printf("%s=%s\n", k, v)
		}
	default:
		die("unknown env command: %s (valid: set, get, rm, list)", args[0])
	}
}
