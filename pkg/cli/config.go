package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

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
		if len(args) > 1 {
			val, err := strconv.ParseFloat(args[1], 64)
			if err != nil || val <= 0 || val > 100 {
				die("invalid threshold value (must be 0-100)")
			}
			cfg.ThresholdPct = val
			if err := identity.SaveConfig("", cfg); err != nil {
				die("save config: %v", err)
			}
			fmt.Printf("✓ Updated threshold_pct to %.1f%%\n", val)
		} else {
			fmt.Printf("threshold_pct = %.1f%%\n", cfg.ThresholdPct)
		}
	default:
		die("unknown config property: %s (supported: threshold)", sub)
	}
}
