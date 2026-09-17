package router

import (
	"strings"

	"amux-accounts/pkg/guard"
	"amux-accounts/pkg/types"
)

// Proxy role labels for Claude-IDE failover layers (Claude subscription
// is never a proxy layer — rotator handles that separately).
const (
	RoleCoding   = "coding"   // Codex
	RoleAnalysis = "analysis" // AGY / Antigravity
	RoleReview   = "review"   // Claude web
	RoleCompact  = "compact"  // soft-prefer ChatGPT web; real shrink is ctxshrink/TaskCompact
	RoleQuality  = "quality"  // Gemini web — product quality evaluation
	RoleAPI      = "api"      // API-other failover
)

// SessionBalanceOrder is the flat round-robin order for NEW sessions when
// there is no manual `am sw` pin. Claude subscription groups are omitted —
// they must not serve as pool proxies for Claude IDE clients.
var SessionBalanceOrder = []string{
	GroupCodexSub,
	GroupCodexFree,
	GroupAGYSub,
	GroupAGYFree,
	GroupClaudeWeb,
	GroupChatGPTWeb,
	GroupGeminiWeb,
	GroupAPIOther,
}

// RoleForGroup returns the coding-layer role for a pool group.
func RoleForGroup(group string) string {
	switch group {
	case GroupCodexSub, GroupCodexFree:
		return RoleCoding
	case GroupAGYSub, GroupAGYFree:
		return RoleAnalysis
	case GroupClaudeWeb:
		return RoleReview
	case GroupChatGPTWeb:
		return RoleCompact
	case GroupGeminiWeb:
		return RoleQuality
	case GroupAPIOther:
		return RoleAPI
	default:
		return ""
	}
}

// RoleForAdapter resolves the role for a live adapter.
func RoleForAdapter(a types.ProviderAdapter) string {
	return RoleForGroup(DetermineAdapterGroup(a))
}

// IsClaudeSubscriptionGroup is true for Claude OAuth / free subscription
// pool groups. Session balancer never picks these as proxy backends.
func IsClaudeSubscriptionGroup(group string) bool {
	return group == GroupClaudeSub || group == GroupClaudeFree
}

// ProxyGroupsForClient returns group try-order for the provider pool when
// the client is Claude IDE: skip Claude subscription (rotator-only), put
// proxy layers first, then API.
func ProxyGroupsForClient(ide string) []string {
	if ide != IDEClaude {
		return GroupPriorityForIDE(ide)
	}
	out := make([]string, 0, len(SessionBalanceOrder))
	seen := make(map[string]bool, len(SessionBalanceOrder))
	for _, g := range SessionBalanceOrder {
		if IsClaudeSubscriptionGroup(g) {
			continue
		}
		out = append(out, g)
		seen[g] = true
	}
	for _, g := range GroupPriority {
		if seen[g] || IsClaudeSubscriptionGroup(g) {
			continue
		}
		out = append(out, g)
	}
	return out
}

func balanceRank(group string) int {
	for i, g := range SessionBalanceOrder {
		if g == group {
			return i
		}
	}
	return len(SessionBalanceOrder) + 1
}

// sortForSessionBalance orders adapters by SessionBalanceOrder, preserving
// relative order within the same group.
func sortForSessionBalance(adapters []types.ProviderAdapter) []types.ProviderAdapter {
	out := make([]types.ProviderAdapter, len(adapters))
	copy(out, adapters)
	// Stable insertion by rank (small N).
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && balanceRank(DetermineAdapterGroup(out[j])) < balanceRank(DetermineAdapterGroup(out[j-1])) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

func isPrimaryProxyLayer(group string) bool {
	switch group {
	case GroupCodexSub, GroupCodexFree, GroupAGYSub, GroupAGYFree,
		GroupClaudeWeb, GroupChatGPTWeb, GroupGeminiWeb:
		return true
	default:
		return false
	}
}

// livingForSessionBalance returns adapters eligible for a new session
// assignment: not Claude-sub, not cooling/quarantined, and usable for the
// request's tool needs. Soft task preference reorders; never excludes a
// living group. Falls back across all eligible adapters.
func (r *AccountPoolRouter) livingForSessionBalance(adapters []types.ProviderAdapter, req *types.ChatRequest, nativeAvailable, strongerThanFree bool) []types.ProviderAdapter {
	var living []types.ProviderAdapter
	for _, a := range adapters {
		grp := DetermineAdapterGroup(a)
		if IsClaudeSubscriptionGroup(grp) && req != nil && req.ClientDialect == "claude" {
			continue
		}
		if shouldSkipAdapter(a, req, nativeAvailable, strongerThanFree) {
			continue
		}
		if isQ, _, _ := guard.IsQuarantined(a.ID()); isQ {
			continue
		}
		if r.cooling(a.ID()) {
			continue
		}
		living = append(living, a)
	}
	order := GroupOrderForRequest(req)
	if len(order) == 0 {
		order = SessionBalanceOrder
	}
	return sortAdaptersByGroupOrder(living, order)
}

// AdapterIDPrefix matches common pool id shapes for role lookup by string.
func AdapterIDPrefix(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch {
	case strings.HasPrefix(id, "codex"):
		return "codex"
	case strings.HasPrefix(id, "agy"), strings.HasPrefix(id, "antigravity"):
		return "agy"
	case strings.Contains(id, "claude:web"), strings.Contains(id, "claude_web"):
		return "claude_web"
	case strings.HasPrefix(id, "chatgpt"):
		return "chatgpt"
	case strings.Contains(id, "gemini:web"), strings.Contains(id, "gemini_web"):
		return "gemini_web"
	default:
		return "api"
	}
}
