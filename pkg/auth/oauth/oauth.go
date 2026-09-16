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

// OAuthOptions configures the OAuth authentication flow.
type OAuthOptions struct {
	CustomName string
	DeviceFlow bool
	ManualFlow bool
}

// RunOAuth starts the standalone OAuth flow with default settings.
func RunOAuth(ctx context.Context, target string, customName string) (accountOrID string, err error) {
	return RunOAuthWithOptions(ctx, target, OAuthOptions{CustomName: customName})
}

// RunOAuthWithOptions starts the standalone OAuth flow supporting device and manual modes.
func RunOAuthWithOptions(ctx context.Context, target string, opts OAuthOptions) (accountOrID string, err error) {
	t := strings.ToLower(strings.TrimSpace(target))

	switch t {
	case "claude", "claudecode", "claude-code":
		if opts.DeviceFlow || opts.ManualFlow {
			_, email, err := LoginClaudeCodeManual(ctx, opts.CustomName)
			if err != nil {
				return "", err
			}
			return email, nil
		}
		_, email, err := LoginClaudeCode(ctx, opts.CustomName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "codex", "codex-cli", "openai":
		if opts.DeviceFlow {
			email, err := LoginCodexDeviceFlow(ctx, opts.CustomName)
			if err != nil {
				return "", err
			}
			return email, nil
		}
		email, err := LoginCodex(ctx, opts.CustomName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "antigravity", "agy", "google":
		email, err := LoginAntigravity(ctx, opts.CustomName)
		if err != nil {
			return "", err
		}
		return email, nil

	case "kimi", "moonshot":
		id, err := LoginKimiDeviceFlow(ctx, opts.CustomName)
		if err != nil {
			return "", err
		}
		return id, nil

	case "grok", "xai":
		id, err := LoginGrokDeviceFlow(ctx, opts.CustomName)
		if err != nil {
			return "", err
		}
		return id, nil

	default:
		return "", fmt.Errorf("unknown oauth provider %q. Supported: %s", target, strings.Join(SupportedOAuthProviders(), ", "))
	}
}

// InteractiveOAuth executes an interactive OAuth flow.
func InteractiveOAuth(target string, customName string, flags ...string) error {
	opts := OAuthOptions{CustomName: customName}
	for _, f := range flags {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "--device" || f == "-d" {
			opts.DeviceFlow = true
		} else if f == "--manual" || f == "-m" {
			opts.ManualFlow = true
		}
	}
	return InteractiveOAuthWithOptions(target, opts)
}

// InteractiveOAuthWithOptions executes an interactive OAuth flow with the specified options.
func InteractiveOAuthWithOptions(target string, opts OAuthOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	mode := "OAuth"
	if opts.DeviceFlow {
		mode = "Device OAuth"
	} else if opts.ManualFlow {
		mode = "Manual OAuth"
	}
	fmt.Printf("== amux Standalone %s: %s ==\n", mode, strings.ToUpper(target))

	result, err := RunOAuthWithOptions(ctx, target, opts)
	if err != nil {
		return err
	}

	fmt.Printf("\n✓ Successfully authenticated %s (%s)!\n", target, result)
	fmt.Println("Account is now active in your amux pool and proxy daemon.")
	return nil
}
