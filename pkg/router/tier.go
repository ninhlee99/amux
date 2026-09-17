package router

import (
	"strings"

	"amux-accounts/pkg/types"
)

// groupAwareAdapter is implemented by adapters that carry an explicit,
// persisted label from account storage (e.g. "codex_sub", "claude_web").
// It is used only to classify account type for tier ordering — never to
// build a display hierarchy or to bias routing toward a particular IDE.
type groupAwareAdapter interface{ Group() string }

// TierOrder is the strict, cost-based priority for account selection:
//  1. Subscription accounts (maximize fixed monthly cost already paid for)
//  2. Web / free-quota accounts (leverage free quota once subscriptions are exhausted)
//  3. Metered API keys (last resort)
//
// This is the ONLY signal used to order accounts. Selection must never
// consider whether the inbound request carries tool/function calls, nor
// which IDE is asking.
var TierOrder = []types.AccountType{
	types.AccountTypeSubscription,
	types.AccountTypeWeb,
	types.AccountTypeAPIKey,
}

// AdapterAccountType classifies a live adapter into one of the three flat
// account types.
func AdapterAccountType(a types.ProviderAdapter) types.AccountType {
	label := ""
	if ga, ok := a.(groupAwareAdapter); ok {
		label = strings.ToLower(strings.TrimSpace(ga.Group()))
	}
	if label == "" {
		label = strings.ToLower(strings.TrimSpace(a.ID()))
	}
	switch {
	case strings.Contains(label, "web"):
		return types.AccountTypeWeb
	case strings.Contains(label, "free"):
		return types.AccountTypeWeb
	case strings.HasPrefix(label, "chatgpt"):
		return types.AccountTypeWeb
	case strings.HasPrefix(label, "codex"),
		strings.HasPrefix(label, "claude"),
		strings.HasPrefix(label, "agy"),
		strings.HasPrefix(label, "antigravity"),
		strings.Contains(label, "_sub"):
		return types.AccountTypeSubscription
	default:
		return types.AccountTypeAPIKey
	}
}

// TierDisplayName is a clean human-readable label for logs/status.
func TierDisplayName(t types.AccountType) string {
	switch t {
	case types.AccountTypeSubscription:
		return "Subscription"
	case types.AccountTypeWeb:
		return "Web"
	case types.AccountTypeAPIKey:
		return "API Key"
	default:
		return string(t)
	}
}
