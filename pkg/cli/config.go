package cli

import (
	"encoding/json"
	"fmt"

	"amux-accounts/pkg/identity"
)

// CmdConfig inspects or modifies runtime configuration.
func CmdConfig(args []string) {
	cfg, err := identity.LoadConfig("")
	if err != nil {
		die("load config: %v", err)
	}

	if len(args) == 0 {
		b, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(b))
		return
	}

	sub := args[0]
	switch sub {
	case "threshold", "threshold_pct":
		if len(args) == 1 {
			fmt.Printf("threshold_pct = %.1f%%\n", cfg.ThresholdPct)
		} else {
			cmdIDThreshold(args[1:])
		}
	default:
		die("unknown config property: %s (supported: threshold)", sub)
	}
}
