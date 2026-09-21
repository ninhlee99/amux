package cli

import (
	"fmt"
	"os"
)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "amux: "+format+"\n", a...)
	os.Exit(1)
}

// Run executes the amux CLI command using the domain-based router.
func Run(rawArgs []string) {
	router := NewRouter()
	router.Dispatch(rawArgs)
}

