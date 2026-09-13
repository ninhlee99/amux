package oauth

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SupportedOAuthProviders returns the list of all supported standalone OAuth targets.
func SupportedOAuthProviders() []string {
	return []string{
		"claude",
		"codex",
		"antigravity",
		"agy",
		"kimi",
		"grok",
	}
}

// RunOAuth starts the standalone OAuth flow for the requested provider without needing
// local CLI or IDE installations.
func RunOAuth(ctx context.Context, target string, customName string) (accountOrID string, err error) {
	t := strings.ToLower(strings.TrimSpace(target))

	switch t {
	case "claude", "claudecode", "claude-code":
		_, email, err := LoginClaudeCode(ctx, customName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "codex", "codex-cli", "openai":
		email, err := LoginCodex(ctx, customName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "antigravity", "agy", "google":
		email, err := LoginAntigravity(ctx, customName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "kimi", "moonshot":
		id, err := LoginKimiDeviceFlow(ctx, customName)
		if err != nil {
			return "", err
		}
		return id, nil

	case "grok", "xai":
		id, err := LoginGrokDeviceFlow(ctx, customName)
		if err != nil {
			return "", err
		}
		return id, nil

	default:
		return "", fmt.Errorf("unknown oauth provider %q. Supported: %s", target, strings.Join(SupportedOAuthProviders(), ", "))
	}
}

// InteractiveOAuth executes an interactive OAuth flow with 5-minute timeout.
func InteractiveOAuth(target string, customName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("== amux Standalone OAuth: %s ==\n", strings.ToUpper(target))
	fmt.Println("No CLI or IDE installation required. Initiating direct OAuth authorization…")

	result, err := RunOAuth(ctx, target, customName)
	if err != nil {
		return err
	}

	fmt.Printf("\n✓ Successfully authenticated %s (%s)!\n", target, result)
	fmt.Println("Account is now active in your amux pool and proxy daemon.")
	return nil
}
