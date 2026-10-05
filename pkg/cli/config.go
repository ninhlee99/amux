package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"amux-accounts/pkg/auth"
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
	case "secret-store", "secret_store", "keychain":
		cmdConfigSecretStore(args[1:])
	default:
		die("unknown config property: %s (supported: threshold, secret-store)", sub)
	}
}

// cmdConfigSecretStore shows or switches where amux keeps secrets:
// "keychain" (macOS Keychain; enables gateway-less native rotation) or
// "file" (AES-256-GCM vault in ~/.amux; never touches the OS keychain).
func cmdConfigSecretStore(args []string) {
	if len(args) == 0 {
		mode := auth.SecretStore()
		fmt.Printf("secret_store = %s\n", mode)
		if mode == auth.SecretStoreFile {
			fmt.Println("  Secrets: ~/.amux/secrets.vault (AES-256-GCM, key ~/.amux/master.key or $AMUX_MASTER_KEY). No keychain access.")
			fmt.Println("  IDEs authenticate to the amux gateway — wire them with `amux hook --all` (or `amux mcp install all`).")
		} else {
			fmt.Println("  Secrets: macOS Keychain. Switch with `amux config secret-store file` to stop all keychain prompts.")
		}
		return
	}
	res, err := auth.SetSecretStore(args[0])
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("secret_store = %s\n", res.Mode)
	if res.MasterKeyMoved {
		fmt.Println("  Exported the encryption key to ~/.amux/master.key (0600) so existing encrypted files stay readable.")
	}
	if len(res.Copied) > 0 {
		fmt.Printf("  Copied into the vault: %s\n", strings.Join(res.Copied, ", "))
	}
	if res.Mode == auth.SecretStoreFile {
		fmt.Println("  amux will no longer read or write the OS keychain.")
		fmt.Println("  Claude Code on macOS reads its own login from the keychain: route it through the gateway with `amux hook --claude`.")
	}
}
